package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	// Exchange 名称
	DelayExchange  = "order.delay.exchange"
	ExpireExchange = "order.expire.exchange"
	NotifyExchange = "order.notify.exchange"

	// Queue 名称
	DelayQueue  = "order.delay.queue"
	ExpireQueue = "order.expire.queue"
	NotifyQueue = "order.notify.queue"

	// Routing Key
	DelayRoutingKey  = "order.delay"
	ExpireRoutingKey = "order.expire"
	NotifyRoutingKey = "order.notify"

	// 重连配置
	reconnectDelay = 3 * time.Second  // 重连间隔
	maxReconnect   = 0                // 最大重连次数，0 表示无限重连
)

// OrderNotifyMessage MQ 通知消息
type OrderNotifyMessage struct {
	OrderNo        string `json:"order_no"`
	OriginalAmount string `json:"original_amount"`
	ActualAmount   string `json:"actual_amount"`
	Status         int16  `json:"status"`
	WalletAddress  string `json:"wallet_address"`
	TxID           string `json:"tx_id"`
	Timestamp      int64  `json:"timestamp"`
}

// RabbitMQ 封装（支持自动重连）
type RabbitMQ struct {
	url           string
	expireMinutes int

	conn    *amqp.Connection
	channel *amqp.Channel

	mu          sync.RWMutex
	isConnected bool
	done        chan struct{}
}

// NewRabbitMQ 创建 RabbitMQ 连接并声明队列
func NewRabbitMQ(url string, expireMinutes int) (*RabbitMQ, error) {
	r := &RabbitMQ{
		url:           url,
		expireMinutes: expireMinutes,
		done:          make(chan struct{}),
	}

	if err := r.connect(); err != nil {
		return nil, err
	}

	// 启动连接监控 goroutine
	go r.monitorConnection()

	return r, nil
}

// connect 建立连接
func (r *RabbitMQ) connect() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	conn, err := amqp.Dial(r.url)
	if err != nil {
		return fmt.Errorf("连接 RabbitMQ 失败: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("打开 Channel 失败: %w", err)
	}

	r.conn = conn
	r.channel = ch
	r.isConnected = true

	if err := r.declareTopology(); err != nil {
		ch.Close()
		conn.Close()
		r.isConnected = false
		return err
	}

	log.Println("RabbitMQ 连接成功")
	return nil
}

// monitorConnection 监控连接状态，断开时自动重连
func (r *RabbitMQ) monitorConnection() {
	for {
		select {
		case <-r.done:
			return
		default:
		}

		r.mu.RLock()
		conn := r.conn
		r.mu.RUnlock()

		if conn == nil {
			time.Sleep(reconnectDelay)
			continue
		}

		// 监听连接关闭事件
		notifyClose := conn.NotifyClose(make(chan *amqp.Error, 1))

		select {
		case <-r.done:
			return
		case err := <-notifyClose:
			if err != nil {
				log.Printf("RabbitMQ 连接断开: %v", err)
			}

			r.mu.Lock()
			r.isConnected = false
			r.mu.Unlock()

			// 开始重连
			r.reconnect()
		}
	}
}

// reconnect 重连逻辑
func (r *RabbitMQ) reconnect() {
	attempt := 0
	for {
		select {
		case <-r.done:
			return
		default:
		}

		attempt++
		log.Printf("RabbitMQ 尝试重连 (第 %d 次)...", attempt)

		if err := r.connect(); err != nil {
			log.Printf("RabbitMQ 重连失败: %v", err)
			time.Sleep(reconnectDelay)

			if maxReconnect > 0 && attempt >= maxReconnect {
				log.Printf("RabbitMQ 重连次数达到上限 (%d)，停止重连", maxReconnect)
				return
			}
			continue
		}

		log.Println("RabbitMQ 重连成功")
		return
	}
}

// declareTopology 声明所有 exchange 和 queue
func (r *RabbitMQ) declareTopology() error {
	// 1. 声明过期处理交换机（direct）
	if err := r.channel.ExchangeDeclare(ExpireExchange, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("声明 ExpireExchange 失败: %w", err)
	}

	// 2. 声明通知交换机（direct）
	if err := r.channel.ExchangeDeclare(NotifyExchange, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("声明 NotifyExchange 失败: %w", err)
	}

	// 3. 声明延时交换机（direct）
	if err := r.channel.ExchangeDeclare(DelayExchange, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("声明 DelayExchange 失败: %w", err)
	}

	// 4. 声明延时队列（带 TTL 和 DLX）
	ttlMs := int32(r.expireMinutes * 60 * 1000)
	_, err := r.channel.QueueDeclare(DelayQueue, true, false, false, false, amqp.Table{
		"x-message-ttl":             ttlMs,
		"x-dead-letter-exchange":    ExpireExchange,
		"x-dead-letter-routing-key": ExpireRoutingKey,
	})
	if err != nil {
		return fmt.Errorf("声明 DelayQueue 失败: %w", err)
	}

	// 绑定延时队列到延时交换机
	if err := r.channel.QueueBind(DelayQueue, DelayRoutingKey, DelayExchange, false, nil); err != nil {
		return fmt.Errorf("绑定 DelayQueue 失败: %w", err)
	}

	// 5. 声明过期消费队列
	_, err = r.channel.QueueDeclare(ExpireQueue, true, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("声明 ExpireQueue 失败: %w", err)
	}

	// 绑定过期队列到过期交换机
	if err := r.channel.QueueBind(ExpireQueue, ExpireRoutingKey, ExpireExchange, false, nil); err != nil {
		return fmt.Errorf("绑定 ExpireQueue 失败: %w", err)
	}

	// 6. 声明通知队列
	_, err = r.channel.QueueDeclare(NotifyQueue, true, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("声明 NotifyQueue 失败: %w", err)
	}

	// 绑定通知队列到通知交换机
	if err := r.channel.QueueBind(NotifyQueue, NotifyRoutingKey, NotifyExchange, false, nil); err != nil {
		return fmt.Errorf("绑定 NotifyQueue 失败: %w", err)
	}

	return nil
}

// IsConnected 检查是否已连接
func (r *RabbitMQ) IsConnected() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.isConnected
}

// PublishDelay 发送延时消息（订单过期检查）
func (r *RabbitMQ) PublishDelay(orderNo string) error {
	r.mu.RLock()
	if !r.isConnected {
		r.mu.RUnlock()
		return fmt.Errorf("RabbitMQ 未连接")
	}
	ch := r.channel
	r.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	body, err := json.Marshal(map[string]string{"order_no": orderNo})
	if err != nil {
		return err
	}

	return ch.PublishWithContext(ctx, DelayExchange, DelayRoutingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		Body:         body,
		DeliveryMode: amqp.Persistent,
	})
}

// PublishNotify 发送订单通知消息（支付成功/过期）
func (r *RabbitMQ) PublishNotify(msg *OrderNotifyMessage) error {
	r.mu.RLock()
	if !r.isConnected {
		r.mu.RUnlock()
		return fmt.Errorf("RabbitMQ 未连接")
	}
	ch := r.channel
	r.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return ch.PublishWithContext(ctx, NotifyExchange, NotifyRoutingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		Body:         body,
		DeliveryMode: amqp.Persistent,
	})
}

// ConsumeExpire 消费过期队列
func (r *RabbitMQ) ConsumeExpire() (<-chan amqp.Delivery, error) {
	r.mu.RLock()
	if !r.isConnected {
		r.mu.RUnlock()
		return nil, fmt.Errorf("RabbitMQ 未连接")
	}
	ch := r.channel
	r.mu.RUnlock()

	// 使用唯一的 consumer tag，避免重连时冲突
	consumerTag := fmt.Sprintf("expire-consumer-%d", time.Now().UnixNano())
	return ch.Consume(ExpireQueue, consumerTag, false, false, false, false, nil)
}

// Close 关闭连接
func (r *RabbitMQ) Close() {
	close(r.done)

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.channel != nil {
		if err := r.channel.Close(); err != nil {
			log.Printf("关闭 RabbitMQ channel 失败: %v", err)
		}
	}
	if r.conn != nil {
		if err := r.conn.Close(); err != nil {
			log.Printf("关闭 RabbitMQ 连接失败: %v", err)
		}
	}
	r.isConnected = false
}
