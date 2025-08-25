package domain

import "time"

type Direction int

const (
	In  Direction = iota
	Out
)

type Transaction struct {
	AccountID        int
	EmailID          string
	TxDate           time.Time
	TxAmount         float64
	TxCurrency       string
	TxDirection      Direction
	TxDesc           string
	Merchant         string
	UserNotes        string
	// Account matching info from statement filename
	StatementAccountNumber *string
	StatementAccountType   string
}