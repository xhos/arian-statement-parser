package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"arian-statement-parser/internal/client"
	pb "arian-statement-parser/internal/gen/arian/v1"
	"arian-statement-parser/internal/parser"

	"github.com/joho/godotenv"
)

func findMatchingAccount(accounts []*pb.Account, accountNumber *string, accountType string) *pb.Account {
	if accountNumber == nil {
		return nil
	}

	// Define expected type based on account type from filename
	var expectedType pb.AccountType
	switch accountType {
	case "visa":
		expectedType = pb.AccountType_ACCOUNT_CREDIT_CARD
	case "savings":
		expectedType = pb.AccountType_ACCOUNT_SAVINGS
	case "chequing":
		expectedType = pb.AccountType_ACCOUNT_CHEQUING
	default:
		return nil
	}

	// First try: Look for accounts where the name equals or ends with the account number
	for _, account := range accounts {
		if (account.Name == *accountNumber || strings.HasSuffix(account.Name, *accountNumber)) && account.Type == expectedType {
			return account
		}
	}

	// Second try: Match by account name for named accounts (Daily, Savings, Student, etc.)
	for _, account := range accounts {
		if account.Type == expectedType {
			// For chequing accounts, match "Daily" to daily statements, "Student" to student statements
			if accountType == "chequing" {
				if strings.Contains(strings.ToLower(account.Name), "daily") {
					return account
				}
				if strings.Contains(strings.ToLower(account.Name), "student") {
					return account
				}
			}
			// For savings accounts, match "Savings" name
			if accountType == "savings" && strings.Contains(strings.ToLower(account.Name), "savings") {
				return account
			}
		}
	}

	// Third try: Match by name only, ignoring type (for existing accounts with wrong/unspecified types)
	for _, account := range accounts {
		if account.Name == *accountNumber || strings.HasSuffix(account.Name, *accountNumber) {
			return account
		}
	}

	return nil
}

func extractAccountName(filePath string) string {
	fileName := filepath.Base(filePath)

	// Extract first word from filename (e.g., "Daily" from "Daily Statement-3878 2024-04-12.pdf")
	words := strings.Fields(fileName)
	if len(words) > 0 {
		return words[0]
	}

	return "Unknown"
}

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: could not load .env file: %v", err)
	}

	// Get configuration from environment variables
	pdfPath := os.Getenv("PDF_PATH")
	if pdfPath == "" {
		pdfPath = "input" // Default to input directory
	}

	configPath := os.Getenv("CONFIG_PATH") // Optional

	userID := os.Getenv("USER_ID")
	if userID == "" {
		fmt.Fprintf(os.Stderr, "Error: USER_ID environment variable is required\n")
		os.Exit(1)
	}

	serverURL := os.Getenv("ARIAND_URL")
	if serverURL == "" {
		fmt.Fprintf(os.Stderr, "Error: ARIAND_URL environment variable is required\n")
		os.Exit(1)
	}

	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		fmt.Fprintf(os.Stderr, "Error: API_KEY environment variable is required\n")
		os.Exit(1)
	}

	// Initialize Python parser
	pythonParser := parser.NewPythonParser()

	// Parse statements using Python script
	fmt.Printf("Parsing PDF statements from: %s\n", pdfPath)
	parseResult, transactions, err := pythonParser.ParseStatements(pdfPath, configPath)
	if err != nil {
		log.Fatalf("Failed to parse statements: %v", err)
	}

	// Display file processing results
	fmt.Println("\n=== File Processing Summary ===")
	fmt.Printf("Total files found: %d\n", parseResult.Summary.TotalFiles)
	fmt.Printf("Successfully processed: %d\n", parseResult.Summary.ProcessedFiles)
	fmt.Printf("Total transactions: %d\n", parseResult.Summary.TotalTransactions)

	fmt.Println("\n=== File Details ===")
	for _, fileResult := range parseResult.FileResults {
		fileName := filepath.Base(fileResult.File)
		status := "✅ PROCESSED"
		if !fileResult.Processed {
			status = "❌ SKIPPED"
		}
		fmt.Printf("%s - %s (%d transactions)\n", fileName, status, fileResult.TransactionCount)
	}

	if parseResult.Summary.ProcessedFiles < parseResult.Summary.TotalFiles {
		fmt.Println("\n⚠️  WARNING: Some files were not processed!")
		fmt.Println("This usually means the file names don't match expected patterns:")
		fmt.Println("- VISA statements should contain 'visa' anywhere in the filename")
		fmt.Println("- Other statements are determined by first word: Daily/Student = chequing, Savings = savings")
		fmt.Println("- All statements need 'Statement-NNNN' pattern for account number extraction")
		fmt.Println("- File names are case-insensitive")
	}

	if len(transactions) == 0 {
		fmt.Println("\nNo transactions found to upload")
		return
	}

	// Ask for user confirmation
	fmt.Printf("\nProceed to upload %d transactions to Arian? (y/N): ", len(transactions))
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		log.Fatalf("Failed to read user input: %v", err)
	}

	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "yes" {
		fmt.Println("Upload cancelled by user")
		return
	}

	// Initialize gRPC client
	fmt.Printf("Connecting to Arian server at: %s\n", serverURL)
	arianClient, err := client.NewClient(serverURL, "", apiKey)
	if err != nil {
		log.Fatalf("Failed to create Arian client: %v", err)
	}
	defer arianClient.Close()

	// Verify user exists
	user, err := arianClient.GetUser(userID)
	if err != nil {
		log.Fatalf("Failed to get user: %v", err)
	}
	displayName := "Unknown"
	if user.DisplayName != nil {
		displayName = *user.DisplayName
	}
	fmt.Printf("Connected as user: %s (%s)\n", displayName, user.Email)

	// Get user accounts
	accounts, err := arianClient.GetAccounts(userID)
	if err != nil {
		log.Fatalf("Failed to get accounts: %v", err)
	}

	if len(accounts) == 0 {
		fmt.Printf("No accounts found for user %s. Accounts will be created automatically during processing.\n", userID)
	}

	// Show available accounts
	fmt.Println("\n=== Available Accounts ===")
	for _, account := range accounts {
		fmt.Printf("- %s (%s)\n", account.Name, account.Type)
	}

	// Upload transactions with account matching
	var successCount, errorCount, unmatchedCount int
	accountMatchStats := make(map[string]int)

	for i, tx := range transactions {
		fmt.Printf("Uploading transaction %d/%d: %.2f %s - %s\n",
			i+1, len(transactions), tx.TxAmount, tx.TxCurrency, tx.TxDesc)

		// Try to match the account
		matchedAccount := findMatchingAccount(accounts, tx.StatementAccountNumber, tx.StatementAccountType)
		if matchedAccount != nil {
			tx.AccountID = int(matchedAccount.Id)
			accountKey := fmt.Sprintf("%s (%s)", matchedAccount.Name, matchedAccount.Type)
			accountMatchStats[accountKey]++
			fmt.Printf("  → Matched to account: %s\n", matchedAccount.Name)
		} else {
			// Create new account with correct type
			accountName := extractAccountName(tx.SourceFilePath)

			var accountType pb.AccountType
			switch tx.StatementAccountType {
			case "visa":
				accountType = pb.AccountType_ACCOUNT_CREDIT_CARD
			case "savings":
				accountType = pb.AccountType_ACCOUNT_SAVINGS
			case "chequing":
				accountType = pb.AccountType_ACCOUNT_CHEQUING
			default:
				accountType = pb.AccountType_ACCOUNT_UNSPECIFIED
			}

			fmt.Printf("  → Creating new account: %s (%s) at RBC\n", accountName, tx.StatementAccountType)
			newAccount, err := arianClient.CreateAccount(userID, accountName, "RBC", accountType) // TODO: Support multiple banks instead of hardcoding RBC
			if err != nil {
				if strings.Contains(err.Error(), "duplicate key value") {
					// Account already exists, refresh list and try again
					fmt.Printf("  → Account already exists, refreshing account list...\n")
					accounts, err = arianClient.GetAccounts(userID)
					if err != nil {
						log.Fatalf("Failed to refresh accounts after duplicate key error: %v", err)
					}
					// Debug: show what we have after refresh
					fmt.Printf("  → After refresh, looking for account number='%s', type='%s'\n",
						func() string {
							if tx.StatementAccountNumber != nil {
								return *tx.StatementAccountNumber
							}
							return "nil"
						}(), tx.StatementAccountType)
					fmt.Printf("  → Available accounts after refresh:\n")
					for _, acc := range accounts {
						fmt.Printf("    - '%s' (type: %s)\n", acc.Name, acc.Type)
					}

					// Try matching again with refreshed list
					matchedAccount := findMatchingAccount(accounts, tx.StatementAccountNumber, tx.StatementAccountType)
					if matchedAccount != nil {
						tx.AccountID = int(matchedAccount.Id)
						accountKey := fmt.Sprintf("%s (%s)", matchedAccount.Name, matchedAccount.Type)
						accountMatchStats[accountKey]++
						fmt.Printf("  → Found existing account: %s\n", matchedAccount.Name)
					} else {
						fmt.Printf("  → ERROR: Still couldn't find account after refresh, skipping transaction\n")
						unmatchedCount++
						continue
					}
				} else {
					log.Fatalf("Failed to create account: %v", err)
				}
			} else {
				tx.AccountID = int(newAccount.Id)
				accountKey := fmt.Sprintf("%s (%s)", newAccount.Name, newAccount.Type)
				accountMatchStats[accountKey]++
				fmt.Printf("  → Successfully created account: %s (%s)\n", newAccount.Name, newAccount.Type)
				// Add new account to accounts slice for future transactions
				accounts = append(accounts, newAccount)
			}
		}

		if err := arianClient.CreateTransaction(userID, tx); err != nil {
			fmt.Printf("  Error: %v\n", err)
			errorCount++
		} else {
			fmt.Printf("  Success!\n")
			successCount++
		}
	}

	// Show account matching statistics
	if len(accountMatchStats) > 0 {
		fmt.Println("\n=== Account Matching Summary ===")
		for account, count := range accountMatchStats {
			fmt.Printf("- %s: %d transactions\n", account, count)
		}
		if unmatchedCount > 0 {
			fmt.Printf("- Unmatched (using default): %d transactions\n", unmatchedCount)
		}
	}

	fmt.Printf("\nUpload complete: %d successful, %d errors\n", successCount, errorCount)
}
