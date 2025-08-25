package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"arian-statement-parser/internal/client"
	"arian-statement-parser/internal/parser"
	pb "arian-statement-parser/internal/gen/arian/v1"

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
	
	// First try: Look for accounts where the name ends with the account number
	for _, account := range accounts {
		if strings.HasSuffix(account.Name, *accountNumber) && account.Type == expectedType {
			return account
		}
	}
	
	// Second try: Match by account type only (for accounts with generic names like "Daily", "Savings")
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
	
	return nil
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

	serverURL := os.Getenv("GRPC_SERVER")
	if serverURL == "" {
		serverURL = "api.arian.xhos.dev:443"
	}
	// Remove https:// prefix if present (gRPC doesn't use HTTP URLs)
	serverURL = strings.TrimPrefix(serverURL, "https://")
	serverURL = strings.TrimPrefix(serverURL, "http://")
	// Add default port if not specified
	if !strings.Contains(serverURL, ":") {
		serverURL += ":443"
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
		fmt.Println("- VISA statements should contain 'visa', 'ion', or 'statement' in filename")
		fmt.Println("- Chequing statements should contain 'chequing' and 'statement' in filename")
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
		log.Fatalf("No accounts found for user %s", userID)
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
			// Fall back to first account if no match
			tx.AccountID = int(accounts[0].Id)
			unmatchedCount++
			accountNumber := "unknown"
			if tx.StatementAccountNumber != nil {
				accountNumber = *tx.StatementAccountNumber
			}
			fmt.Printf("  ⚠️  No account match for %s (%s), using default: %s\n", 
				accountNumber, tx.StatementAccountType, accounts[0].Name)
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