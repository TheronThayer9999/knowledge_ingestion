package postgres

import (
	"context"
	"knowledge_ingestion/src/config"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"gorm.io/gorm"
)

type testConfig struct {
	inner config.IConfig
	host  string
}

func (c *testConfig) GetApp() config.AppConfig { return c.inner.GetApp() }

func (c *testConfig) GetDatabase() config.DBConfig {
	db := c.inner.GetDatabase()
	if c.host != "" {
		db.Host = c.host
	}
	return db
}

func (c *testConfig) GetStorage() config.S3Config { return c.inner.GetStorage() }

func loadTestConfig(t *testing.T) config.IConfig {
	t.Helper()
	path := os.Getenv("APP_CONFIG_PATH")
	if path == "" {
		path = filepath.Join("..", "..", "..", "configs", "config.json")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Skipf("config not available: %v", err)
	}
	return cfg
}

func reachableHost(db config.DBConfig) string {
	candidates := []string{db.Host}
	if db.Host != "127.0.0.1" && db.Host != "localhost" {
		candidates = append(candidates, "127.0.0.1")
	}
	for _, host := range candidates {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(db.Port)))
		cancel()
		if err == nil {
			err := conn.Close()
			if err != nil {
				return ""
			}
			return host
		}
	}
	return ""
}

func TestPostgresConnection(t *testing.T) {
	cfg := loadTestConfig(t)
	db := cfg.GetDatabase()

	host := reachableHost(db)
	if host == "" {
		t.Skipf("postgres not reachable at %s:5432 (nor 127.0.0.1)", db.Host)
	}
	if host != db.Host {
		t.Logf("config host %q not resolvable, falling back to %q", db.Host, host)
	}

	conn, err := NewConnection(&testConfig{inner: cfg, host: host})
	if err != nil {
		t.Fatalf("NewConnection failed: %v", err)
	}
	defer func(db *gorm.DB) {
		_, err := db.DB()
		if err != nil {
			t.Errorf("db.DB() failed: %v", err)
		}
	}(conn.GetDB())

	var version string
	if err := conn.GetDB().Raw("SELECT version()").Row().Scan(&version); err != nil {
		t.Fatalf("query failed: %v", err)
	}
	t.Logf("connected: %s", version)
}
