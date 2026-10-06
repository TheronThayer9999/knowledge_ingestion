package caches

import (
	"context"
	"fmt"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/config"
	"time"

	"github.com/redis/go-redis/v9"
)

type ICache interface {
	GetClient() *redis.Client
}

type cacheWrapper struct {
	client *redis.Client
}

func (w *cacheWrapper) GetClient() *redis.Client {
	return w.client
}

func NewConnection(cfg config.IConfig) (ICache, error) {
	redisCfg := cfg.GetRedis()

	rdb := redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", redisCfg.Host, redisCfg.Port),
		Password:     redisCfg.Password,
		DB:           redisCfg.DB,
		ReadTimeout:  time.Duration(redisCfg.SocketTimeout) * time.Second,
		WriteTimeout: time.Duration(redisCfg.SocketTimeout) * time.Second,
		MaxRetries:   3,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Kiểm tra kết nối
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to ping redis: %w", err)
	}

	logs.Infow("redis connected", "host", redisCfg.Host, "port", redisCfg.Port)

	return &cacheWrapper{client: rdb}, nil
}
