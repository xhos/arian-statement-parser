package mapping

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AccountMapping represents a mapping between a statement account number and an ariand account ID
type AccountMapping struct {
	StatementAccountNumber string `json:"statement_account_number"`
	StatementAccountType   string `json:"statement_account_type"`
	ArianAccountID         string `json:"arian_account_id"`
	ArianAccountName       string `json:"arian_account_name"`
}

// Store manages account mappings
type Store struct {
	filePath string
	Mappings []AccountMapping `json:"mappings"`
}

// NewStore creates a new mapping store
func NewStore() (*Store, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".config", "arian-statement-parser")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	filePath := filepath.Join(configDir, "account-mappings.json")

	store := &Store{
		filePath: filePath,
		Mappings: []AccountMapping{},
	}

	// Load existing mappings if file exists
	if _, err := os.Stat(filePath); err == nil {
		if err := store.Load(); err != nil {
			return nil, err
		}
	}

	return store, nil
}

// Load reads mappings from disk
func (s *Store) Load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return fmt.Errorf("failed to read mappings file: %w", err)
	}

	if err := json.Unmarshal(data, &s.Mappings); err != nil {
		return fmt.Errorf("failed to parse mappings: %w", err)
	}

	return nil
}

// Save writes mappings to disk
func (s *Store) Save() error {
	data, err := json.MarshalIndent(s.Mappings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal mappings: %w", err)
	}

	if err := os.WriteFile(s.filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write mappings file: %w", err)
	}

	return nil
}

// FindMapping looks up an existing mapping
func (s *Store) FindMapping(statementAccountNumber, statementAccountType string) *AccountMapping {
	for i := range s.Mappings {
		if s.Mappings[i].StatementAccountNumber == statementAccountNumber &&
			s.Mappings[i].StatementAccountType == statementAccountType {
			return &s.Mappings[i]
		}
	}
	return nil
}

// AddMapping adds a new mapping
func (s *Store) AddMapping(mapping AccountMapping) error {
	// Check if mapping already exists and update it
	for i := range s.Mappings {
		if s.Mappings[i].StatementAccountNumber == mapping.StatementAccountNumber &&
			s.Mappings[i].StatementAccountType == mapping.StatementAccountType {
			s.Mappings[i] = mapping
			return s.Save()
		}
	}

	// Add new mapping
	s.Mappings = append(s.Mappings, mapping)
	return s.Save()
}
