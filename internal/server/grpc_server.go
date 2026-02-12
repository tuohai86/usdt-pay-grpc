package server

import (
	"context"

	"usdt-pay-grpc/internal/service"
	pb "usdt-pay-grpc/proto/order"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type UsdtPayServer struct {
	pb.UnimplementedUsdtPayServiceServer
	orderService *service.OrderService
}

func NewUsdtPayServer(orderService *service.OrderService) *UsdtPayServer {
	return &UsdtPayServer{
		orderService: orderService,
	}
}

func (s *UsdtPayServer) CreateOrder(ctx context.Context, req *pb.CreateOrderRequest) (*pb.CreateOrderResponse, error) {
	if req.Amount == "" {
		return nil, status.Error(codes.InvalidArgument, "金额不能为空")
	}

	order, err := s.orderService.CreateOrder(ctx, req.Amount)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "创建订单失败: %v", err)
	}

	return &pb.CreateOrderResponse{
		OrderNo:        order.OrderNo,
		ActualAmount:   order.ActualAmount.String(),
		OriginalAmount: order.OriginalAmount.String(),
		WalletAddress:  order.WalletAddress,
	}, nil
}
