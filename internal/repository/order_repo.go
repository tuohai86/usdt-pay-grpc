package repository

import (
	"usdt-pay-grpc/internal/model"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type OrderRepository struct {
	db *gorm.DB
}

func NewOrderRepository(db *gorm.DB) *OrderRepository {
	return &OrderRepository{db: db}
}

// FindPendingActualAmounts 在事务中查询指定原始金额下所有待支付订单的实际金额（带行锁）
func (r *OrderRepository) FindPendingActualAmounts(tx *gorm.DB, originalAmount decimal.Decimal) ([]decimal.Decimal, error) {
	var amounts []decimal.Decimal
	err := tx.Model(&model.Order{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("original_amount = ? AND status = ?", originalAmount, model.OrderStatusPending).
		Pluck("actual_amount", &amounts).Error
	return amounts, err
}

// Create 创建订单
func (r *OrderRepository) Create(tx *gorm.DB, order *model.Order) error {
	return tx.Create(order).Error
}

// FindByActualAmountAndStatus 根据实际金额和状态查找订单
func (r *OrderRepository) FindByActualAmountAndStatus(actualAmount decimal.Decimal, status int16) (*model.Order, error) {
	var order model.Order
	err := r.db.Where("actual_amount = ? AND status = ?", actualAmount, status).First(&order).Error
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// UpdateStatus 更新订单状态
func (r *OrderRepository) UpdateStatus(orderID int64, status int16) error {
	return r.db.Model(&model.Order{}).Where("id = ?", orderID).Update("status", status).Error
}

// UpdateStatusWithTxHash 更新订单状态和交易哈希
func (r *OrderRepository) UpdateStatusWithTxHash(orderID int64, status int16, txHash string) error {
	return r.db.Model(&model.Order{}).Where("id = ?", orderID).
		Updates(map[string]interface{}{"status": status, "tx_hash": txHash}).Error
}

// FindByOrderNo 根据订单号查找订单
func (r *OrderRepository) FindByOrderNo(orderNo string) (*model.Order, error) {
	var order model.Order
	err := r.db.Where("order_no = ?", orderNo).First(&order).Error
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// GetDB 获取数据库实例（用于事务）
func (r *OrderRepository) GetDB() *gorm.DB {
	return r.db
}

// OrderFilter 订单查询过滤条件
type OrderFilter struct {
	OrderNo       string
	Status        *int16
	WalletAddress string
	TxHash        string
	Page          int
	PageSize      int
}

// ListOrders 分页查询订单列表，支持多字段搜索
func (r *OrderRepository) ListOrders(filter OrderFilter) ([]model.Order, int64, error) {
	query := r.db.Model(&model.Order{})

	// 订单号模糊匹配
	if filter.OrderNo != "" {
		query = query.Where("order_no LIKE ?", "%"+filter.OrderNo+"%")
	}
	// 状态精确匹配
	if filter.Status != nil {
		query = query.Where("status = ?", *filter.Status)
	}
	// 钱包地址精确匹配
	if filter.WalletAddress != "" {
		query = query.Where("wallet_address = ?", filter.WalletAddress)
	}
	// 交易哈希精确匹配
	if filter.TxHash != "" {
		query = query.Where("tx_hash = ?", filter.TxHash)
	}

	// 查询总数
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页参数默认值
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}

	offset := (filter.Page - 1) * filter.PageSize

	var orders []model.Order
	if err := query.Order("created_at DESC").Offset(offset).Limit(filter.PageSize).Find(&orders).Error; err != nil {
		return nil, 0, err
	}

	return orders, total, nil
}
