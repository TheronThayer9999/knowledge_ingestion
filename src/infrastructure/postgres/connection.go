package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"knowledge_ingestion/src/config"
	"log"
	"time"

	"go.uber.org/fx"
)

// IDB định nghĩa contract cho database access
// Service/Repo layer sẽ phụ thuộc vào interface này thay vì *sql.DB trực tiếp
type IDB interface {
	GetDB() *sql.DB
}

// dbWrapper implements IDB
type dbWrapper struct {
	db *sql.DB
}

func (w *dbWrapper) GetDB() *sql.DB {
	return w.db
}

// NewConnection là FX Provider cho PostgreSQL
// Nó tự động nhận IConfig từ FX container
func NewConnection(cfg config.IConfig) (IDB, error) {
	dbCfg := cfg.GetDatabase()

	// Build connection string an toàn
	connStr := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		dbCfg.Host, dbCfg.Port, dbCfg.User, dbCfg.Password, dbCfg.DBName, dbCfg.SSLMode,
	)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres connection: %w", err)
	}

	// Cấu hình connection pool phù hợp với RAG/Ingestion workload
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Test kết nối ngay lúc startup (fail fast)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	log.Printf("✅ PostgreSQL connected: %s:%d/%s", dbCfg.Host, dbCfg.Port, dbCfg.DBName)

	return &dbWrapper{db: db}, nil
}

// Module gom nhóm tất cả provider liên quan đến Postgres
var Module = fx.Options(
	fx.Provide(NewConnection),
)
