package trongrid

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	USDTContract = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
)

// TRC20Transaction 表示 TronGrid 返回的 TRC20 交易
type TRC20Transaction struct {
	TransactionID  string `json:"transaction_id"`
	TokenInfo      TokenInfo `json:"token_info"`
	From           string `json:"from"`
	To             string `json:"to"`
	Type           string `json:"type"`
	Value          string `json:"value"`
	BlockTimestamp int64  `json:"block_timestamp"`
}

type TokenInfo struct {
	Symbol   string `json:"symbol"`
	Address  string `json:"address"`
	Decimals int    `json:"decimals"`
	Name     string `json:"name"`
}

// TRC20Response TronGrid API 响应
type TRC20Response struct {
	Data    []TRC20Transaction `json:"data"`
	Success bool               `json:"success"`
	Meta    Meta               `json:"meta"`
}

type Meta struct {
	At          int64  `json:"at"`
	Fingerprint string `json:"fingerprint"`
	PageSize    int    `json:"page_size"`
}

// Client TronGrid HTTP 客户端
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewClient 创建 TronGrid 客户端
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetTRC20Transactions 获取指定地址的 TRC20 交易列表
func (c *Client) GetTRC20Transactions(address string, minTimestamp int64) ([]TRC20Transaction, error) {
	url := fmt.Sprintf("%s/v1/accounts/%s/transactions/trc20?limit=200&order_by=block_timestamp,desc", c.baseURL, address)
	if minTimestamp > 0 {
		url += fmt.Sprintf("&min_timestamp=%d", minTimestamp)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("TRON-PRO-API-KEY", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 TronGrid 失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TronGrid 返回错误状态码: %d, body: %s", resp.StatusCode, string(body))
	}

	var result TRC20Response
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("TronGrid 返回失败")
	}

	return result.Data, nil
}
