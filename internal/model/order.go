package model

import (
	"time"

	"github.com/shopspring/decimal"
)

const (
	OrderStatusPending = 0 // 待支付
	OrderStatusPaid    = 1 // 已支付
	OrderStatusExpired = 2 // 已过期
)

type Order struct {
	ID             int64           `gorm:"primaryKey;autoIncrement" json:"id"`
	OrderNo        string          `gorm:"type:varchar(18);uniqueIndex;not null" json:"order_no"`
	OriginalAmount decimal.Decimal `gorm:"type:decimal(20,2);not null" json:"original_amount"`
	ActualAmount   decimal.Decimal `gorm:"type:decimal(20,2);not null;index:idx_actual_status" json:"actual_amount"`
	WalletAddress  string          `gorm:"type:varchar(64);not null" json:"wallet_address"`
	Status         int16           `gorm:"type:smallint;not null;default:0;index:idx_actual_status;index:idx_status_expired" json:"status"`
	TxHash         string          `gorm:"type:varchar(128);default:''" json:"tx_hash"`
	CreatedAt      time.Time       `gorm:"autoCreateTime" json:"created_at"`
	ExpiredAt      time.Time       `gorm:"index:idx_status_expired;not null" json:"expired_at"`
}

func (Order) TableName() string {
	return "orders"
}
