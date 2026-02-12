package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"usdt-pay-grpc/internal/config"
	"usdt-pay-grpc/internal/model"
	"usdt-pay-grpc/internal/mq"

	amqp "github.com/rabbitmq/amqp091-go"
	pb "usdt-pay-grpc/proto/order"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("========== USDT Pay gRPC 端到端测试 ==========")

	// 1. 加载配置（获取 gRPC 端口和 RabbitMQ 地址）
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 2. 连接 gRPC 服务
	grpcAddr := fmt.Sprintf("localhost:%d", cfg.GRPCPort)
	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("连接 gRPC 服务失败: %v", err)
	}
	defer conn.Close()
	log.Printf("[OK] gRPC 连接成功 (%s)", grpcAddr)

	client := pb.NewUsdtPayServiceClient(conn)

	// 3. 连接 RabbitMQ（用于消费通知）
	amqpConn, err := amqp.Dial(cfg.RabbitMQURL)
	if err != nil {
		log.Fatalf("连接 RabbitMQ 失败: %v", err)
	}
	defer amqpConn.Close()

	amqpCh, err := amqpConn.Channel()
	if err != nil {
		log.Fatalf("打开 RabbitMQ Channel 失败: %v", err)
	}
	defer amqpCh.Close()
	log.Println("[OK] RabbitMQ 连接成功")

	// 4. 通过 gRPC 创建订单（金额 10 USDT）
	log.Println("")
	log.Println("==========================================")
	log.Println("  步骤 1: 通过 gRPC 创建订单 (金额: 10 USDT)")
	log.Println("==========================================")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	resp, err := client.CreateOrder(ctx, &pb.CreateOrderRequest{Amount: "10"})
	cancel()
	if err != nil {
		log.Fatalf("gRPC CreateOrder 失败: %v", err)
	}

	log.Println("[OK] 订单创建成功!")
	log.Printf("    订单号:     %s", resp.OrderNo)
	log.Printf("    原始金额:   %s USDT", resp.OriginalAmount)
	log.Printf("    实际金额:   %s USDT", resp.ActualAmount)
	log.Printf("    钱包地址:   %s", resp.WalletAddress)

	// 5. 启动通知消费者，监听支付成功 / 过期事件
	log.Println("")
	log.Println("==========================================")
	log.Println("  步骤 2: 监听通知队列 (支付成功 / 过期)")
	log.Println("==========================================")
	log.Printf("  - 订单将在 %d 分钟后过期", cfg.OrderExpireMinutes)
	log.Println("  - 如果链上收到对应金额转账 -> 支付成功通知")
	log.Println("  - 如果超时未支付 -> 订单过期通知")
	log.Println("  - 按 Ctrl+C 退出")
	log.Println("")

	runCtx, runCancel := context.WithCancel(context.Background())
	defer runCancel()

	go consumeNotify(runCtx, amqpCh, resp.OrderNo)

	// 6. 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("")
	log.Println("收到退出信号，正在关闭...")
	runCancel()
	time.Sleep(500 * time.Millisecond)
	log.Println("测试已退出")
}

// consumeNotify 消费通知队列，过滤出当前订单的通知
func consumeNotify(ctx context.Context, ch *amqp.Channel, targetOrderNo string) {
	consumerTag := fmt.Sprintf("test-notify-%d", time.Now().UnixNano())
	msgs, err := ch.Consume(mq.NotifyQueue, consumerTag, false, false, false, false, nil)
	if err != nil {
		log.Printf("订阅通知队列失败: %v", err)
		return
	}

	log.Println("[OK] 通知队列订阅成功，等待消息...")

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgs:
			if !ok {
				log.Println("通知消费通道已关闭")
				return
			}

			var notify mq.OrderNotifyMessage
			if err := json.Unmarshal(msg.Body, &notify); err != nil {
				log.Printf("解析通知消息失败: %v", err)
				msg.Nack(false, false)
				continue
			}

			// 只关注当前测试创建的订单
			if notify.OrderNo != targetOrderNo {
				log.Printf("  (忽略其他订单通知: %s)", notify.OrderNo)
				msg.Ack(false)
				continue
			}

			log.Println("")
			log.Println("**************************************************")
			switch notify.Status {
			case model.OrderStatusPaid:
				log.Println("  >>> 支付成功!")
			case model.OrderStatusExpired:
				log.Println("  >>> 订单已过期!")
			default:
				log.Printf("  >>> 未知状态: %d", notify.Status)
			}
			log.Printf("    订单号:     %s", notify.OrderNo)
			log.Printf("    原始金额:   %s USDT", notify.OriginalAmount)
			log.Printf("    实际金额:   %s USDT", notify.ActualAmount)
			log.Printf("    状态:       %s", statusText(notify.Status))
			log.Printf("    钱包地址:   %s", notify.WalletAddress)
			if notify.TxID != "" {
				log.Printf("    交易哈希:   %s", notify.TxID)
			}
			log.Printf("    时间戳:     %s", time.Unix(notify.Timestamp, 0).Format("2006-01-02 15:04:05"))
			log.Println("**************************************************")
			log.Println("")

			msg.Ack(false)
		}
	}
}

// statusText 订单状态文本
func statusText(status int16) string {
	switch status {
	case model.OrderStatusPending:
		return "待支付 (Pending)"
	case model.OrderStatusPaid:
		return "已支付 (Paid)"
	case model.OrderStatusExpired:
		return "已过期 (Expired)"
	default:
		return fmt.Sprintf("未知 (%d)", status)
	}
}
