package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"time"

	"usdt-pay-grpc/internal/model"
	"usdt-pay-grpc/internal/mq"
	"usdt-pay-grpc/internal/repository"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/shopspring/decimal"
)

type OrderService struct {
	orderRepo     *repository.OrderRepository
	mqClient      *mq.RabbitMQ
	walletAddress string
	expireMinutes int
}

func NewOrderService(
	orderRepo *repository.OrderRepository,
	mqClient *mq.RabbitMQ,
	walletAddress string,
	expireMinutes int,
) *OrderService {
	return &OrderService{
		orderRepo:     orderRepo,
		mqClient:      mqClient,
		walletAddress: walletAddress,
		expireMinutes: expireMinutes,
	}
}

// CreateOrder 创建 USDT 支付订单
func (s *OrderService) CreateOrder(ctx context.Context, amountStr string) (*model.Order, error) {
	originalAmount, err := decimal.NewFromString(amountStr)
	if err != nil {
		return nil, fmt.Errorf("无效的金额: %w", err)
	}

	if originalAmount.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("金额必须大于 0")
	}

	// 开始数据库事务
	tx := s.orderRepo.GetDB().Begin()
	if tx.Error != nil {
		return nil, fmt.Errorf("开启事务失败: %w", tx.Error)
	}

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 查询所有待支付且原始金额相同的订单（带行锁）
	usedAmounts, err := s.orderRepo.FindPendingActualAmounts(tx, originalAmount)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("查询已用金额失败: %w", err)
	}

	// 计算可用的实际支付金额
	actualAmount := s.findAvailableAmount(originalAmount, usedAmounts)

	// 生成订单号: YYYYMMDDHHmmss + 4位随机数
	orderNo := s.generateOrderNo()

	now := time.Now()
	order := &model.Order{
		OrderNo:        orderNo,
		OriginalAmount: originalAmount,
		ActualAmount:   actualAmount,
		WalletAddress:  s.walletAddress,
		Status:         model.OrderStatusPending,
		CreatedAt:      now,
		ExpiredAt:      now.Add(time.Duration(s.expireMinutes) * time.Minute),
	}

	// 插入订单
	if err := s.orderRepo.Create(tx, order); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("创建订单失败: %w", err)
	}

	// 提交事务
	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("提交事务失败: %w", err)
	}

	// 发送延时消息到 RabbitMQ（用于过期检查）
	if err := s.mqClient.PublishDelay(order.OrderNo); err != nil {
		log.Printf("发送延时消息失败 (订单: %s): %v", order.OrderNo, err)
	}

	return order, nil
}

// HandleExpiredOrder 处理过期订单（由 MQ 消费者调用）
func (s *OrderService) HandleExpiredOrder(orderNo string) error {
	order, err := s.orderRepo.FindByOrderNo(orderNo)
	if err != nil {
		return fmt.Errorf("查询订单失败: %w", err)
	}

	// 只处理仍为待支付的订单
	if order.Status != model.OrderStatusPending {
		return nil
	}

	// 更新为已过期
	if err := s.orderRepo.UpdateStatus(order.ID, model.OrderStatusExpired); err != nil {
		return fmt.Errorf("更新订单状态失败: %w", err)
	}

	// 发送过期通知到 MQ
	notifyMsg := &mq.OrderNotifyMessage{
		OrderNo:        order.OrderNo,
		OriginalAmount: order.OriginalAmount.String(),
		ActualAmount:   order.ActualAmount.String(),
		Status:         model.OrderStatusExpired,
		WalletAddress:  order.WalletAddress,
		Timestamp:      time.Now().Unix(),
	}

	if err := s.mqClient.PublishNotify(notifyMsg); err != nil {
		log.Printf("发送过期通知失败 (订单: %s): %v", order.OrderNo, err)
	}

	log.Printf("订单已过期: %s", order.OrderNo)
	return nil
}

// StartExpireConsumer 启动过期消息消费者（支持自动重连后重新订阅）
func (s *OrderService) StartExpireConsumer(ctx context.Context) error {
	go s.runExpireConsumer(ctx)
	log.Println("过期消息消费者已启动")
	return nil
}

// runExpireConsumer 运行消费者，支持重连后重新订阅
func (s *OrderService) runExpireConsumer(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Println("过期消费者已停止")
			return
		default:
		}

		// 等待 MQ 连接就绪
		if !s.mqClient.IsConnected() {
			time.Sleep(time.Second)
			continue
		}

		msgs, err := s.mqClient.ConsumeExpire()
		if err != nil {
			log.Printf("订阅过期队列失败: %v，等待重连...", err)
			time.Sleep(2 * time.Second)
			continue
		}

		log.Println("过期队列消费者订阅成功")

		// 消费消息，直到通道关闭
		s.consumeMessages(ctx, msgs)

		// 通道关闭后等待一段时间再重新订阅，避免频繁重试
		log.Println("过期消费通道已关闭，等待重连...")
		time.Sleep(2 * time.Second)
	}
}

// consumeMessages 消费消息，直到 ctx 取消或通道关闭
func (s *OrderService) consumeMessages(ctx context.Context, msgs <-chan amqp.Delivery) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgs:
			if !ok {
				// 通道关闭，返回让外层重新订阅
				return
			}

			var data struct {
				OrderNo string `json:"order_no"`
			}
			if err := json.Unmarshal(msg.Body, &data); err != nil {
				log.Printf("解析过期消息失败: %v", err)
				msg.Nack(false, false)
				continue
			}

			if err := s.HandleExpiredOrder(data.OrderNo); err != nil {
				log.Printf("处理过期订单失败 (%s): %v", data.OrderNo, err)
				msg.Nack(false, true) // requeue
				continue
			}

			msg.Ack(false)
		}
	}
}

// findAvailableAmount 查找可用的实际支付金额
// 从原始金额开始，每次 +0.01，直到找到未被使用的金额
func (s *OrderService) findAvailableAmount(originalAmount decimal.Decimal, usedAmounts []decimal.Decimal) decimal.Decimal {
	increment := decimal.NewFromFloat(0.01)
	usedSet := make(map[string]bool, len(usedAmounts))
	for _, a := range usedAmounts {
		usedSet[a.String()] = true
	}

	candidate := originalAmount
	for {
		if !usedSet[candidate.String()] {
			return candidate
		}
		candidate = candidate.Add(increment)
	}
}

// generateOrderNo 生成订单号: YYYYMMDDHHmmss + 4位随机数
func (s *OrderService) generateOrderNo() string {
	now := time.Now()
	prefix := now.Format("20060102150405")
	suffix := fmt.Sprintf("%04d", rand.Intn(10000))
	return prefix + suffix
}
