package service

import (
	"context"
	"log"
	"strings"
	"time"

	"usdt-pay-grpc/internal/model"
	"usdt-pay-grpc/internal/mq"
	"usdt-pay-grpc/internal/repository"
	"usdt-pay-grpc/pkg/trongrid"

	"github.com/shopspring/decimal"
)

type ScannerService struct {
	tronClient  *trongrid.Client
	orderRepo   *repository.OrderRepository
	txRepo      *repository.TransactionRepository
	mqClient    *mq.RabbitMQ
	wallet      string
	interval    time.Duration
}

func NewScannerService(
	tronClient *trongrid.Client,
	orderRepo *repository.OrderRepository,
	txRepo *repository.TransactionRepository,
	mqClient *mq.RabbitMQ,
	walletAddress string,
	intervalSec int,
) *ScannerService {
	return &ScannerService{
		tronClient: tronClient,
		orderRepo:  orderRepo,
		txRepo:     txRepo,
		mqClient:   mqClient,
		wallet:     walletAddress,
		interval:   time.Duration(intervalSec) * time.Second,
	}
}

// Start 启动扫描服务
func (s *ScannerService) Start(ctx context.Context) {
	log.Printf("TronGrid 扫描服务已启动，间隔: %v", s.interval)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// 启动时立即扫描一次
	s.scan()

	for {
		select {
		case <-ctx.Done():
			log.Println("TronGrid 扫描服务已停止")
			return
		case <-ticker.C:
			s.scan()
		}
	}
}

// scan 执行一次扫描
func (s *ScannerService) scan() {
	// 获取最近 1 小时的交易（覆盖 30 分钟过期窗口）
	minTimestamp := time.Now().Add(-1 * time.Hour).UnixMilli()

	txList, err := s.tronClient.GetTRC20Transactions(s.wallet, minTimestamp)
	if err != nil {
		log.Printf("获取 TRC20 交易失败: %v", err)
		return
	}

	for _, tx := range txList {
		s.processTransaction(tx)
	}
}

// processTransaction 处理单笔交易
func (s *ScannerService) processTransaction(tx trongrid.TRC20Transaction) {
	// 1. 必须是 USDT 合约
	if tx.TokenInfo.Address != trongrid.USDTContract {
		return
	}

	// 2. 必须是转入到本钱包
	if !strings.EqualFold(tx.To, s.wallet) {
		return
	}

	// 3. 必须是 Transfer 类型
	if tx.Type != "Transfer" {
		return
	}

	// 4. 检查是否已扫描过
	exists, err := s.txRepo.ExistsByTxID(tx.TransactionID)
	if err != nil {
		log.Printf("检查交易是否存在失败 (tx: %s): %v", tx.TransactionID, err)
		return
	}
	if exists {
		return
	}

	// 5. 解析金额（USDT 6 位小数，value 是最小单位）
	amount, err := s.parseAmount(tx.Value, tx.TokenInfo.Decimals)
	if err != nil {
		log.Printf("解析金额失败 (tx: %s, value: %s): %v", tx.TransactionID, tx.Value, err)
		return
	}

	// 6. 保存交易记录（去重）
	scannedTx := &model.ScannedTransaction{
		TxID:           tx.TransactionID,
		FromAddress:    tx.From,
		ToAddress:      tx.To,
		Amount:         amount,
		BlockTimestamp: tx.BlockTimestamp,
	}
	if err := s.txRepo.Create(scannedTx); err != nil {
		log.Printf("保存交易记录失败 (tx: %s): %v", tx.TransactionID, err)
		return
	}

	// 7. 匹配待支付订单
	order, err := s.orderRepo.FindByActualAmountAndStatus(amount, model.OrderStatusPending)
	if err != nil {
		log.Printf("未找到匹配订单 (tx: %s, amount: %s)", tx.TransactionID, amount.String())
		return
	}

	// 8. 更新订单为已支付（同时写入交易哈希）
	if err := s.orderRepo.UpdateStatusWithTxHash(order.ID, model.OrderStatusPaid, tx.TransactionID); err != nil {
		log.Printf("更新订单状态失败 (订单: %s): %v", order.OrderNo, err)
		return
	}

	// 9. 发送支付成功通知到 MQ
	notifyMsg := &mq.OrderNotifyMessage{
		OrderNo:        order.OrderNo,
		OriginalAmount: order.OriginalAmount.String(),
		ActualAmount:   order.ActualAmount.String(),
		Status:         model.OrderStatusPaid,
		WalletAddress:  order.WalletAddress,
		TxID:           tx.TransactionID,
		Timestamp:      time.Now().Unix(),
	}

	if err := s.mqClient.PublishNotify(notifyMsg); err != nil {
		log.Printf("发送支付通知失败 (订单: %s): %v", order.OrderNo, err)
	}

	log.Printf("订单支付成功: %s, 金额: %s, 交易: %s", order.OrderNo, amount.String(), tx.TransactionID)
}

// parseAmount 将 TronGrid 返回的 value（最小单位字符串）转为实际金额
func (s *ScannerService) parseAmount(value string, decimals int) (decimal.Decimal, error) {
	raw, err := decimal.NewFromString(value)
	if err != nil {
		return decimal.Decimal{}, err
	}
	divisor := decimal.New(1, int32(decimals))
	return raw.Div(divisor), nil
}
