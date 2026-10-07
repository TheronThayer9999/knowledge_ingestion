package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// IConfig định nghĩa contract cho việc lấy cấu hình
type IConfig interface {
	GetApp() AppConfig
	GetDatabase() DBConfig
	GetStorage() S3Config
	GetEmbedding() EmbeddingConfig
	GetRedis() RedisConfig // Thêm method này
	GetRabbitMQ() RabbitMQConfig
	GetJWT() JWTConfig
	GetCORS() CorsConfig
}

// Implementations
type appConfig struct {
	App       AppConfig       `json:"app"`
	Database  DBConfig        `json:"database"`
	Storage   S3Config        `json:"storage"`
	Embedding EmbeddingConfig `json:"embedding"`
	RedisCli  RedisConfig     `json:"redis"`
	Rabbit    RabbitMQConfig  `json:"rabbitmq"`
	JWTCli    JWTConfig       `json:"jwt"`
	Cors      CorsConfig      `json:"cors"`
}

type AppConfig struct {
	Name string `json:"name"`
	Port int    `json:"port"`
	Env  string `json:"env"`
}

type DBConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	DBName   string `json:"dbname"`
	SSLMode  string `json:"sslmode"`
}

type S3Config struct {
	Type      string `json:"type"`
	Endpoint  string `json:"s3_endpoint"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Bucket    string `json:"bucket_name"`
}

type EmbeddingConfig struct {
	Provider string `json:"provider"` // "ollama", "openai", ...
	Model    string `json:"model"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"` // Ollama để trống
	Dim      int    `json:"dim"`
}

// redis cli
type RedisConfig struct {
	Host           string `json:"host"` // Đổi từ Addr sang Host để dễ quản lý
	Port           int    `json:"port"` // Thêm Port
	Password       string `json:"password"`
	DB             int    `json:"db"`
	SocketTimeout  int    `json:"socket_timeout"`
	RetryOnTimeout bool   `json:"retry_on_timeout"`
}

// RabbitMQConfig giữ kết nối broker + topology cho relay outbox (worker
// publish/consume event). Khớp docker/rabbit_mq/docker-compose.yml
// (user admin/admin123, vhost /, AMQP 5672, management UI 15672).
type RabbitMQConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Vhost    string `json:"vhost"`
	// Exchange worker tự declare lúc boot (direct, durable) — relay publish
	// event vào đây, queue bind theo event_type.
	Exchange string `json:"exchange"`
	// Queue hàng đợi chính của relay — worker tự declare + bind lúc boot.
	Queue string `json:"queue"`
}

// JWTConfig giữ bí mật ký token và thời hạn sống (phút). Secret ưu tiên
// lấy từ env JWT_SECRET để production không commit secret vào file.
type JWTConfig struct {
	Secret        string `json:"secret"`
	ExpiryMinutes int    `json:"expiry_minutes"`
}

// CorsConfig giữ danh sách origin được phép gọi API (Angular dev/prod).
type CorsConfig struct {
	AllowedOrigins []string `json:"allowed_origins"`
}

// Load đọc file JSON và trả về IConfig
func Load(path string) (IConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	var cfg appConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	// Validate cơ bản
	if cfg.App.Port == 0 {
		return nil, fmt.Errorf("app.port is required")
	}
	if cfg.Database.Host == "" {
		return nil, fmt.Errorf("database.host is required")
	}
	if cfg.Embedding.Provider == "" {
		return nil, fmt.Errorf("embedding.provider is required")
	}
	if cfg.Embedding.Model == "" {
		return nil, fmt.Errorf("embedding.model is required")
	}
	if cfg.Embedding.Dim <= 0 {
		return nil, fmt.Errorf("embedding.dim must be > 0")
	}
	if cfg.RedisCli.Host == "" {
		return nil, fmt.Errorf("redis.host is required")
	}
	if cfg.Rabbit.Host == "" {
		return nil, fmt.Errorf("rabbitmq.host is required")
	}
	if cfg.Rabbit.Port == 0 {
		cfg.Rabbit.Port = 5672 // AMQP mặc định
	}
	if cfg.Rabbit.User == "" {
		cfg.Rabbit.User = "guest"
	}
	// Password ưu tiên env để production không commit secret vào file
	if envPass := os.Getenv("RABBITMQ_PASSWORD"); envPass != "" {
		cfg.Rabbit.Password = envPass
	}
	if cfg.Rabbit.Vhost == "" {
		cfg.Rabbit.Vhost = "/"
	}
	if cfg.Rabbit.Exchange == "" {
		cfg.Rabbit.Exchange = "knowledge.events"
	}
	if cfg.Rabbit.Queue == "" {
		cfg.Rabbit.Queue = "knowledge.outbox"
	}
	// Secret ưu tiên env để production không phải commit secret vào file
	if envSecret := os.Getenv("JWT_SECRET"); envSecret != "" {
		cfg.JWTCli.Secret = envSecret
	}
	if cfg.JWTCli.Secret == "" {
		return nil, fmt.Errorf("jwt.secret is required (hoặc env JWT_SECRET)")
	}
	if cfg.JWTCli.ExpiryMinutes <= 0 {
		cfg.JWTCli.ExpiryMinutes = 1440 // mặc định 1 ngày
	}
	if len(cfg.Cors.AllowedOrigins) == 0 {
		cfg.Cors.AllowedOrigins = []string{"http://localhost:4200"} // mặc định Angular dev
	}

	return &cfg, nil
}

// Getter methods
func (c *appConfig) GetApp() AppConfig             { return c.App }
func (c *appConfig) GetDatabase() DBConfig         { return c.Database }
func (c *appConfig) GetStorage() S3Config          { return c.Storage }
func (c *appConfig) GetEmbedding() EmbeddingConfig { return c.Embedding }
func (c *appConfig) GetRedis() RedisConfig         { return c.RedisCli }
func (c *appConfig) GetRabbitMQ() RabbitMQConfig   { return c.Rabbit }
func (c *appConfig) GetJWT() JWTConfig             { return c.JWTCli }
func (c *appConfig) GetCORS() CorsConfig           { return c.Cors }
