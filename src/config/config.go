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
}

// Implementations
type appConfig struct {
	App      AppConfig `json:"app"`
	Database DBConfig  `json:"database"`
	Storage  S3Config  `json:"storage"`
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

// Load đọc file JSON và trả về IConfig
func Load(path string) (IConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	var cfg appConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Validate cơ bản
	if cfg.App.Port == 0 {
		return nil, fmt.Errorf("app.port is required")
	}
	if cfg.Database.Host == "" {
		return nil, fmt.Errorf("database.host is required")
	}

	return &cfg, nil
}

// Getter methods
func (c *appConfig) GetApp() AppConfig     { return c.App }
func (c *appConfig) GetDatabase() DBConfig { return c.Database }
func (c *appConfig) GetStorage() S3Config  { return c.Storage }
