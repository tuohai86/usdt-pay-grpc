package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"usdt-pay-grpc/internal/config"
	"usdt-pay-grpc/internal/model"
	"usdt-pay-grpc/internal/mq"
	"usdt-pay-grpc/internal/repository"
	"usdt-pay-grpc/internal/server"
	"usdt-pay-grpc/internal/service"
	"usdt-pay-grpc/pkg/trongrid"
	pb "usdt-pay-grpc/proto/order"

	"google.golang.org/grpc"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	// 1. 加载配置
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	log.Printf("配置加载成功, gRPC端口: %d, 钱包地址: %s", cfg.GRPCPort, cfg.WalletAddress)

	// 2. 连接数据库（Silent 模式不输出 SQL，只有错误时输出）
	db, err := gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	log.Println("数据库连接成功")

	// 自动迁移
	if err := db.AutoMigrate(&model.Order{}, &model.ScannedTransaction{}); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}
	log.Println("数据库迁移完成")

	// 3. 连接 RabbitMQ
	mqClient, err := mq.NewRabbitMQ(cfg.RabbitMQURL, cfg.OrderExpireMinutes)
	if err != nil {
		log.Fatalf("连接 RabbitMQ 失败: %v", err)
	}
	defer mqClient.Close()
	log.Println("RabbitMQ 连接成功")

	// 4. 初始化 Repository
	orderRepo := repository.NewOrderRepository(db)
	txRepo := repository.NewTransactionRepository(db)

	// 5. 初始化 Service
	orderService := service.NewOrderService(orderRepo, mqClient, cfg.WalletAddress, cfg.OrderExpireMinutes)
	tronClient := trongrid.NewClient(cfg.TronGridAPIURL, cfg.TronGridAPIKey)
	scannerService := service.NewScannerService(tronClient, orderRepo, txRepo, mqClient, cfg.WalletAddress, cfg.ScanInterval)

	// 6. 创建可取消的 context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 7. 启动过期消息消费者
	if err := orderService.StartExpireConsumer(ctx); err != nil {
		log.Fatalf("启动过期消费者失败: %v", err)
	}

	// 8. 启动 TronGrid 扫描服务
	go scannerService.Start(ctx)

	// 9. 启动 gRPC 服务
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.GRPCPort))
	if err != nil {
		log.Fatalf("监听端口失败: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterUsdtPayServiceServer(grpcServer, server.NewUsdtPayServer(orderService))

	go func() {
		log.Printf("gRPC 服务已启动，监听端口: %d", cfg.GRPCPort)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("gRPC 服务异常: %v", err)
		}
	}()

	// 10. 启动 HTTP 服务
	httpServer := server.NewHTTPServer(orderRepo, cfg.HTTPPort)
	go func() {
		log.Printf("HTTP 服务已启动，监听端口: %d", cfg.HTTPPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP 服务异常: %v", err)
		}
	}()

	// 11. 优雅退出
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Printf("收到退出信号: %v", sig)

	cancel()
	grpcServer.GracefulStop()
	if err := httpServer.Shutdown(context.Background()); err != nil {
		log.Printf("HTTP 服务关闭异常: %v", err)
	}
	log.Println("服务已优雅退出")
}
