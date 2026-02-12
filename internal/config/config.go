package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type Config struct {
	GRPCPort           int    `env:"GRPC_PORT" envDefault:"50051"`
	HTTPPort           int    `env:"HTTP_PORT" envDefault:"8080"`
	DBHost             string `env:"DB_HOST" envDefault:"localhost"`
	DBPort             int    `env:"DB_PORT" envDefault:"5432"`
	DBUser             string `env:"DB_USER" envDefault:"postgres"`
	DBPassword         string `env:"DB_PASSWORD" envDefault:"password"`
	DBName             string `env:"DB_NAME" envDefault:"usdt_pay"`
	RabbitMQURL        string `env:"RABBITMQ_URL" envDefault:"amqp://guest:guest@localhost:5672/"`
	WalletAddress      string `env:"WALLET_ADDRESS,required"`
	TronGridAPIURL     string `env:"TRONGRID_API_URL" envDefault:"https://api.trongrid.io"`
	TronGridAPIKey     string `env:"TRONGRID_API_KEY"`
	ScanInterval       int    `env:"SCAN_INTERVAL" envDefault:"10"`
	OrderExpireMinutes int    `env:"ORDER_EXPIRE_MINUTES" envDefault:"30"`
}

func Load() (*Config, error) {
	_ = godotenv.Load() // 忽略 .env 不存在的错误
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) DSN() string {
	return "host=" + c.DBHost +
		" port=" + itoa(c.DBPort) +
		" user=" + c.DBUser +
		" password=" + c.DBPassword +
		" dbname=" + c.DBName +
		" sslmode=disable TimeZone=Asia/Shanghai"
}

func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}
