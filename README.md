<div align="center">

# USDT Pay gRPC

**基于 Go 的 USDT TRC20 自动收款系统**

通过 gRPC 创建支付订单 · 自动扫描链上交易确认 · RabbitMQ 异步通知

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev/)
[![gRPC](https://img.shields.io/badge/gRPC-Protocol-244c5a?style=flat-square&logo=google&logoColor=white)](https://grpc.io/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-Database-336791?style=flat-square&logo=postgresql&logoColor=white)](https://www.postgresql.org/)
[![RabbitMQ](https://img.shields.io/badge/RabbitMQ-MQ-FF6600?style=flat-square&logo=rabbitmq&logoColor=white)](https://www.rabbitmq.com/)
[![TRON](https://img.shields.io/badge/TRON-TRC20-EB0029?style=flat-square&logo=tron&logoColor=white)](https://tron.network/)
[![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)](LICENSE)

</div>

---

## 核心流程

```
创建订单 (gRPC)  →  用户转账 USDT  →  链上扫描匹配  →  MQ 通知对接方
```

1. 对接方通过 **gRPC** 提交金额，系统返回唯一的实际支付金额（自动避免金额冲突，递增 0.01）
2. 订单创建后自动进入**延时过期队列**（默认 30 分钟）
3. **定时扫描** TronGrid，根据金额精确匹配待支付订单
4. 支付成功或过期均通过 `order.notify.queue` 发送通知

---

## 快速开始

```bash
# 1. 克隆项目
git clone https://github.com/your-repo/usdt-pay-grpc.git
cd usdt-pay-grpc

# 2. 配置环境变量
cp .env.example .env
# 编辑 .env，至少填写 WALLET_ADDRESS 和 TRONGRID_API_KEY

# 3. 启动服务
go run cmd/server/main.go
```

服务启动后：

| 服务 | 端口 | 说明 |
|:---:|:---:|:---:|
| gRPC | `:50051` | 创建订单 |
| HTTP | `:8080` | 查询订单 |

---

## 环境变量

<details>
<summary><b>点击展开完整配置</b></summary>

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `GRPC_PORT` | `50051` | gRPC 端口 |
| `HTTP_PORT` | `8080` | HTTP 端口 |
| `DB_HOST` | `localhost` | PostgreSQL 地址 |
| `DB_PORT` | `5432` | PostgreSQL 端口 |
| `DB_USER` | `postgres` | 数据库用户 |
| `DB_PASSWORD` | `password` | 数据库密码 |
| `DB_NAME` | `usdt_pay` | 数据库名 |
| `RABBITMQ_URL` | `amqp://guest:guest@localhost:5672/` | RabbitMQ 地址 |
| `WALLET_ADDRESS` | - | **必填** USDT TRC20 收款地址 |
| `TRONGRID_API_URL` | `https://api.trongrid.io` | TronGrid API |
| `TRONGRID_API_KEY` | - | TronGrid Key（建议配置） |
| `SCAN_INTERVAL` | `10` | 扫描间隔（秒） |
| `ORDER_EXPIRE_MINUTES` | `30` | 订单过期时间（分钟） |

</details>

---

## gRPC 接口

### CreateOrder — 创建支付订单

```protobuf
service UsdtPayService {
  rpc CreateOrder(CreateOrderRequest) returns (CreateOrderResponse);
}
```

**请求参数：**

| 字段 | 类型 | 说明 |
|:---:|:---:|------|
| `amount` | `string` | 原始支付金额，如 `"100.00"` |

**响应参数：**

| 字段 | 类型 | 说明 |
|:---:|:---:|------|
| `order_no` | `string` | 18 位订单号 |
| `actual_amount` | `string` | 实际需支付金额（展示给用户） |
| `original_amount` | `string` | 原始请求金额 |
| `wallet_address` | `string` | 收款钱包地址 |

<details>
<summary><b>Go 调用示例</b></summary>

```go
conn, _ := grpc.NewClient("localhost:50051",
    grpc.WithTransportCredentials(insecure.NewCredentials()))
defer conn.Close()

client := pb.NewUsdtPayServiceClient(conn)
resp, err := client.CreateOrder(ctx, &pb.CreateOrderRequest{Amount: "100.00"})
// resp.OrderNo, resp.ActualAmount, resp.WalletAddress
```

</details>

---

## HTTP 接口

### `GET /api/orders` — 查询订单列表

支持分页和多字段搜索。

**Query 参数：**

| 参数 | 类型 | 默认值 | 说明 |
|:---:|:---:|:---:|------|
| `page` | `int` | `1` | 页码 |
| `page_size` | `int` | `20` | 每页条数（上限 100） |
| `order_no` | `string` | - | 订单号模糊搜索 |
| `status` | `int` | - | 状态筛选：`0` 待支付 / `1` 已支付 / `2` 已过期 |
| `wallet_address` | `string` | - | 钱包地址精确匹配 |
| `tx_hash` | `string` | - | 交易哈希精确匹配 |

**请求示例：**

```
GET /api/orders?page=1&page_size=20
GET /api/orders?order_no=202602&status=1
GET /api/orders?wallet_address=TXxxxxx&page=2&page_size=10
```

<details>
<summary><b>响应示例</b></summary>

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "total": 100,
    "page": 1,
    "page_size": 20,
    "orders": [
      {
        "id": 1,
        "order_no": "202602121430251234",
        "original_amount": "100.00",
        "actual_amount": "100.01",
        "wallet_address": "TDqSquXBgUCLYvYC4XZgrprLK589dkhSCf",
        "status": 1,
        "status_text": "已支付",
        "tx_hash": "abc123...def456",
        "created_at": "2026-02-12T14:30:25Z",
        "expired_at": "2026-02-12T15:00:25Z"
      }
    ]
  }
}
```

</details>

---

## MQ 通知

对接方需消费队列：`order.notify.queue`

**消息格式：**

```json
{
  "order_no": "202602121430251234",
  "original_amount": "100.00",
  "actual_amount": "100.01",
  "status": 1,
  "wallet_address": "TDqSquXBgUCLYvYC4XZgrprLK589dkhSCf",
  "tx_id": "abc123...def456",
  "timestamp": 1739356225
}
```

| 字段 | 说明 |
|:---:|------|
| `status` | `1` = 支付成功，`2` = 订单过期 |
| `tx_id` | 链上交易哈希（仅支付成功时有值） |

<details>
<summary><b>Go 消费示例</b></summary>

```go
msgs, _ := ch.Consume("order.notify.queue", "my-consumer", false, false, false, false, nil)
for msg := range msgs {
    var notify OrderNotifyMessage
    json.Unmarshal(msg.Body, &notify)
    // 根据 notify.Status 处理业务逻辑
    msg.Ack(false)
}
```

</details>

---

## 数据库结构

### `orders` 表

| 字段 | 类型 | 说明 |
|:---:|:---:|------|
| `id` | `BIGSERIAL` | 主键 |
| `order_no` | `VARCHAR(18)` | 订单号（唯一索引） |
| `original_amount` | `DECIMAL(20,2)` | 原始金额 |
| `actual_amount` | `DECIMAL(20,2)` | 实际金额 |
| `wallet_address` | `VARCHAR(64)` | 收款地址 |
| `status` | `SMALLINT` | 0=待支付, 1=已支付, 2=已过期 |
| `tx_hash` | `VARCHAR(128)` | 交易哈希 |
| `created_at` | `TIMESTAMP` | 创建时间 |
| `expired_at` | `TIMESTAMP` | 过期时间 |

### `scanned_transactions` 表

| 字段 | 类型 | 说明 |
|:---:|:---:|------|
| `id` | `BIGSERIAL` | 主键 |
| `tx_id` | `VARCHAR(128)` | 交易哈希（唯一，去重用） |
| `from_address` | `VARCHAR(64)` | 发送地址 |
| `to_address` | `VARCHAR(64)` | 接收地址 |
| `amount` | `DECIMAL(30,6)` | 交易金额 |
| `block_timestamp` | `BIGINT` | 区块时间戳（ms） |
| `created_at` | `TIMESTAMP` | 记录时间 |

---

## 注意事项

> [!IMPORTANT]
> - **金额必须用字符串**传递，避免浮点精度问题；前端务必展示 `actual_amount`
> - **消息幂等**：通知可能重复投递，对接方应根据 `order_no` 做幂等处理
> - **手动确认**：MQ 消费者应使用 `auto_ack=false`，业务失败时 Nack 重试

> [!TIP]
> **测试网**：可将 `TRONGRID_API_URL` 改为 `https://api.shasta.trongrid.io`（Shasta）或 `https://nile.trongrid.io`（Nile）

---

## 项目结构

```
usdt-pay-grpc/
├── cmd/server/main.go              # 应用入口
├── internal/
│   ├── config/config.go            # 环境变量配置
│   ├── model/                      # 数据模型
│   ├── repository/                 # 数据库操作
│   ├── service/                    # 业务逻辑（订单、链上扫描）
│   ├── mq/rabbitmq.go              # RabbitMQ 封装
│   └── server/
│       ├── grpc_server.go          # gRPC Handler
│       └── http_server.go          # HTTP Handler
├── pkg/trongrid/client.go          # TronGrid 客户端
├── proto/order/                    # Proto 定义及生成代码
└── .env.example                    # 环境变量示例
```

---

## 联系方式

如有问题或合作意向，欢迎通过以下方式联系：

| | |
|:---:|---|
| **Telegram** | [@chajian](https://t.me/chajian) |
| **电话** | +888 0842 6516 |
| **昵称** | 拓海 |

---

## 许可证

本项目基于 [MIT License](LICENSE) 开源。
