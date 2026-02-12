package repository

import (
	"usdt-pay-grpc/internal/model"

	"gorm.io/gorm"
)

type TransactionRepository struct {
	db *gorm.DB
}

func NewTransactionRepository(db *gorm.DB) *TransactionRepository {
	return &TransactionRepository{db: db}
}

// ExistsByTxID 检查交易是否已存在
func (r *TransactionRepository) ExistsByTxID(txID string) (bool, error) {
	var count int64
	err := r.db.Model(&model.ScannedTransaction{}).Where("tx_id = ?", txID).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// Create 保存已扫描的交易
func (r *TransactionRepository) Create(tx *model.ScannedTransaction) error {
	return r.db.Create(tx).Error
}
