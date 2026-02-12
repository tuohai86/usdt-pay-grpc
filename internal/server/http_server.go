package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"usdt-pay-grpc/internal/model"
	"usdt-pay-grpc/internal/repository"
)

// orderResponse 订单响应结构（自定义 JSON 输出）
type orderResponse struct {
	ID             int64  `json:"id"`
	OrderNo        string `json:"order_no"`
	OriginalAmount string `json:"original_amount"`
	ActualAmount   string `json:"actual_amount"`
	WalletAddress  string `json:"wallet_address"`
	Status         int16  `json:"status"`
	StatusText     string `json:"status_text"`
	TxHash         string `json:"tx_hash"`
	CreatedAt      string `json:"created_at"`
	ExpiredAt      string `json:"expired_at"`
}

// apiResponse 统一 API 响应
type apiResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// listData 列表数据
type listData struct {
	Total    int64           `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
	Orders   []orderResponse `json:"orders"`
}

// HTTPHandler HTTP 接口处理器
type HTTPHandler struct {
	orderRepo *repository.OrderRepository
}

// NewHTTPServer 创建并返回 HTTP 服务器
func NewHTTPServer(orderRepo *repository.OrderRepository, port int) *http.Server {
	handler := &HTTPHandler{orderRepo: orderRepo}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/orders", handler.handleOrders)

	return &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}
}

// handleOrders 处理订单列表请求
func (h *HTTPHandler) handleOrders(w http.ResponseWriter, r *http.Request) {
	// CORS 头
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, apiResponse{
			Code:    -1,
			Message: "仅支持 GET 请求",
		})
		return
	}

	query := r.URL.Query()

	// 解析分页参数
	page, _ := strconv.Atoi(query.Get("page"))
	pageSize, _ := strconv.Atoi(query.Get("page_size"))

	// 构建过滤条件
	filter := repository.OrderFilter{
		OrderNo:       query.Get("order_no"),
		WalletAddress: query.Get("wallet_address"),
		TxHash:        query.Get("tx_hash"),
		Page:          page,
		PageSize:      pageSize,
	}

	// 解析 status（可选）
	if statusStr := query.Get("status"); statusStr != "" {
		statusVal, err := strconv.ParseInt(statusStr, 10, 16)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, apiResponse{
				Code:    -1,
				Message: "status 参数无效，应为 0(待支付)、1(已支付)、2(已过期)",
			})
			return
		}
		s := int16(statusVal)
		filter.Status = &s
	}

	// 查询数据
	orders, total, err := h.orderRepo.ListOrders(filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiResponse{
			Code:    -1,
			Message: "查询失败: " + err.Error(),
		})
		return
	}

	// 规范化分页参数（与 Repository 保持一致）
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}

	// 构建响应
	orderList := make([]orderResponse, 0, len(orders))
	for _, o := range orders {
		orderList = append(orderList, toOrderResponse(o))
	}

	writeJSON(w, http.StatusOK, apiResponse{
		Code:    0,
		Message: "success",
		Data: listData{
			Total:    total,
			Page:     filter.Page,
			PageSize: filter.PageSize,
			Orders:   orderList,
		},
	})
}

// toOrderResponse 将 model.Order 转为响应结构
func toOrderResponse(o model.Order) orderResponse {
	return orderResponse{
		ID:             o.ID,
		OrderNo:        o.OrderNo,
		OriginalAmount: o.OriginalAmount.String(),
		ActualAmount:   o.ActualAmount.String(),
		WalletAddress:  o.WalletAddress,
		Status:         o.Status,
		StatusText:     statusText(o.Status),
		TxHash:         o.TxHash,
		CreatedAt:      o.CreatedAt.Format("2006-01-02T15:04:05Z"),
		ExpiredAt:      o.ExpiredAt.Format("2006-01-02T15:04:05Z"),
	}
}

// statusText 将状态码转为中文描述
func statusText(status int16) string {
	switch status {
	case model.OrderStatusPending:
		return "待支付"
	case model.OrderStatusPaid:
		return "已支付"
	case model.OrderStatusExpired:
		return "已过期"
	default:
		return "未知"
	}
}

// writeJSON 写入 JSON 响应
func writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
