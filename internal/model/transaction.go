package model

import (
	"time"

	"github.com/shopspring/decimal"
)

type ScannedTransaction struct {
	ID             int64           `gorm:"primaryKey;autoIncrement" json:"id"`
	TxID           string          `gorm:"type:varchar(128);uniqueIndex;not null" json:"tx_id"`
	FromAddress    string          `gorm:"type:varchar(64);not null" json:"from_address"`
	ToAddress      string          `gorm:"type:varchar(64);not null" json:"to_address"`
	Amount         decimal.Decimal `gorm:"type:decimal(30,6);not null" json:"amount"`
	BlockTimestamp int64           `gorm:"not null" json:"block_timestamp"`
	CreatedAt      time.Time       `gorm:"autoCreateTime" json:"created_at"`
}

func (ScannedTransaction) TableName() string {
	return "scanned_transactions"
}
