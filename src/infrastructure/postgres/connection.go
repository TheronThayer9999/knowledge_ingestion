package postgres

import (
	"context"
	"fmt"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type IDB interface {
	GetDB() *gorm.DB
}

type dbWrapper struct {
	db *gorm.DB
}

func (w *dbWrapper) GetDB() *gorm.DB {
	return w.db
}

func NewConnection(cfg config.IConfig) (IDB, error) {
	dbCfg := cfg.GetDatabase()

	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		dbCfg.Host, dbCfg.Port, dbCfg.User, dbCfg.Password, dbCfg.DBName, dbCfg.SSLMode,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres connection: %w", err)
	}
	err = db.AutoMigrate(domain.User{})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql pool: %w", err)
	}

	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(ctx); err != nil {
		err := sqlDB.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	logs.Infow("postgresql connected", "host", dbCfg.Host, "port", dbCfg.Port, "db", dbCfg.DBName)

	return &dbWrapper{db: db}, nil
}
