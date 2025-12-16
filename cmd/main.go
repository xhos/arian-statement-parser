package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"arian-statement-parser/internal/client"
	pb "arian-statement-parser/internal/gen/arian/v1"
	"arian-statement-parser/internal/mapping"
	"arian-statement-parser/internal/parser"

	"github.com/joho/godotenv"
)

func convertToAccountType(accountType string) pb.AccountType {
	switch accountType {
	case "visa":
		return pb.AccountType_ACCOUNT_CREDIT_CARD
	case "savings":
		return pb.AccountType_ACCOUNT_SAVINGS
	case "chequing":
		return pb.AccountType_ACCOUNT_CHEQUING
	default:
		return pb.AccountType_ACCOUNT_UNSPECIFIED
	}
}

func findMatchingAccount(accounts []*pb.Account, accountName string, accountType string) *pb.Account {
	expectedType := convertToAccountType(accountType)
	for _, account := range accounts {
		if account.Type == expectedType && strings.EqualFold(account.Name, accountName) {
			return account
		}
	}
	return nil
}

func main() {
	pdfPath := flag.String("pdf", "", "")
	configPath := flag.String("config", "", "")
	flag.Parse()

	godotenv.Load()

	if *pdfPath == "" {
		if envPath := os.Getenv("PDF_PATH"); envPath != "" {
			*pdfPath = envPath
		} else {
			fmt.Fprintf(os.Stderr, "need -pdf flag\n")
			os.Exit(1)
		}
	}

	userID := os.Getenv("USER_ID")
	if userID == "" {
		fmt.Fprintf(os.Stderr, "need USER_ID\n")
		os.Exit(1)
	}

	serverURL := os.Getenv("ARIAND_URL")
	if serverURL == "" {
		fmt.Fprintf(os.Stderr, "need ARIAND_URL\n")
		os.Exit(1)
	}

	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		fmt.Fprintf(os.Stderr, "need API_KEY\n")
		os.Exit(1)
	}

	pythonParser := parser.NewPythonParser()

	fmt.Printf("parsing %s\n", *pdfPath)
	parseResult, transactions, err := pythonParser.ParseStatements(*pdfPath, *configPath)
	if err != nil {
		log.Fatalf("parse failed: %v", err)
	}

	fmt.Printf("files: %d/%d, transactions: %d\n",
		parseResult.Summary.ProcessedFiles,
		parseResult.Summary.TotalFiles,
		parseResult.Summary.TotalTransactions)

	for _, fileResult := range parseResult.FileResults {
		fileName := filepath.Base(fileResult.File)
		if fileResult.Processed {
			fmt.Printf("  %s: %d\n", fileName, fileResult.TransactionCount)
		}
	}

	if len(transactions) == 0 {
		return
	}

	fmt.Printf("\nupload %d transactions? (y/N): ", len(transactions))
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		log.Fatalf("read failed: %v", err)
	}

	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "yes" {
		return
	}

	arianClient, err := client.NewClient(serverURL, "", apiKey)
	if err != nil {
		log.Fatalf("client failed: %v", err)
	}
	defer arianClient.Close()

	_, err = arianClient.GetUser(userID)
	if err != nil {
		log.Fatalf("user not found: %v", err)
	}

	accounts, err := arianClient.GetAccounts(userID)
	if err != nil {
		log.Fatalf("get accounts failed: %v", err)
	}

	// Initialize mapping store
	mappingStore, err := mapping.NewStore()
	if err != nil {
		log.Fatalf("failed to initialize mapping store: %v", err)
	}

	var successCount, errorCount int
	accountMatchStats := make(map[string]int)
	askedMappings := make(map[string]bool) // Track which accounts we've already asked about

	for i, tx := range transactions {
		var accountName string
		if tx.StatementAccountNumber != nil && *tx.StatementAccountNumber != "" {
			accountName = *tx.StatementAccountNumber
		} else {
			accountName = "Unknown"
		}

		var matchedAccount *pb.Account

		// First, check if we have a saved mapping for this statement account
		mappingKey := accountName + "|" + tx.StatementAccountType
		savedMapping := mappingStore.FindMapping(accountName, tx.StatementAccountType)

		if savedMapping != nil {
			// Use the saved mapping
			savedAccountID, _ := strconv.ParseInt(savedMapping.ArianAccountID, 10, 64)
			for _, account := range accounts {
				if account.Id == savedAccountID {
					matchedAccount = account
					break
				}
			}
			if matchedAccount == nil {
				log.Printf("WARN: saved mapping for '%s' points to non-existent account, will re-prompt", accountName)
			}
		}

		// If no saved mapping or account not found, try to match by name and type
		if matchedAccount == nil {
			matchedAccount = findMatchingAccount(accounts, accountName, tx.StatementAccountType)
		}

		// If still no match, prompt the user (but only once per unique account)
		if matchedAccount == nil && !askedMappings[mappingKey] {
			askedMappings[mappingKey] = true

			selectedAccountID, isNewAccount, err := mapping.PromptForAccountMapping(accountName, tx.StatementAccountType, accounts)
			if err != nil {
				log.Fatalf("mapping prompt failed: %v", err)
			}

			if isNewAccount {
				// Create new account
				accountType := convertToAccountType(tx.StatementAccountType)
				newAccount, err := arianClient.CreateAccount(userID, accountName, "RBC", accountType)
				if err != nil {
					log.Fatalf("create account failed: %v", err)
				}
				matchedAccount = newAccount
				accounts = append(accounts, newAccount)

				// Save mapping
				err = mappingStore.AddMapping(mapping.AccountMapping{
					StatementAccountNumber: accountName,
					StatementAccountType:   tx.StatementAccountType,
					ArianAccountID:         strconv.FormatInt(newAccount.Id, 10),
					ArianAccountName:       newAccount.Name,
				})
				if err != nil {
					log.Printf("WARN: failed to save mapping: %v", err)
				}
			} else {
				// Use selected existing account
				selectedAccountIDInt, _ := strconv.ParseInt(selectedAccountID, 10, 64)
				for _, account := range accounts {
					if account.Id == selectedAccountIDInt {
						matchedAccount = account
						break
					}
				}

				if matchedAccount == nil {
					log.Fatalf("selected account not found")
				}

				// Save mapping
				err = mappingStore.AddMapping(mapping.AccountMapping{
					StatementAccountNumber: accountName,
					StatementAccountType:   tx.StatementAccountType,
					ArianAccountID:         strconv.FormatInt(matchedAccount.Id, 10),
					ArianAccountName:       matchedAccount.Name,
				})
				if err != nil {
					log.Printf("WARN: failed to save mapping: %v", err)
				}

				// Warn if types don't match
				expectedType := convertToAccountType(tx.StatementAccountType)
				if matchedAccount.Type != expectedType {
					log.Printf("WARN: account '%s' type mismatch - statement expects %s but account is %s (continuing anyway)", accountName, expectedType, matchedAccount.Type)
				}
			}
		}

		if matchedAccount != nil {
			tx.AccountID = int(matchedAccount.Id)
			accountMatchStats[accountName]++
		} else {
			log.Fatalf("no account found for transaction (this shouldn't happen)")
		}

		if err := arianClient.CreateTransaction(userID, tx); err != nil {
			errorCount++
		} else {
			successCount++
		}

		if (i+1)%50 == 0 {
			fmt.Printf("%d/%d\n", i+1, len(transactions))
		}
	}

	fmt.Printf("\n%d ok, %d failed\n", successCount, errorCount)
	for account, count := range accountMatchStats {
		fmt.Printf("  %s: %d\n", account, count)
	}
}
