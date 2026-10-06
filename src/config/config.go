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
}

// Implementations
type appConfig struct {
	App       AppConfig       `json:"app"`
	Database  DBConfig        `json:"database"`
	Storage   S3Config        `json:"storage"`
	Embedding EmbeddingConfig `json:"embedding"`
	RedisCli  RedisConfig     `json:"redis"`
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

	return &cfg, nil
}

// Getter methods
func (c *appConfig) GetApp() AppConfig             { return c.App }
func (c *appConfig) GetDatabase() DBConfig         { return c.Database }
func (c *appConfig) GetStorage() S3Config          { return c.Storage }
func (c *appConfig) GetEmbedding() EmbeddingConfig { return c.Embedding }
func (c *appConfig) GetRedis() RedisConfig         { return c.RedisCli }
