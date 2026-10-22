package services

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"knowledge_ingestion/src/common/storage"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/infrastructure/postgres"
	"knowledge_ingestion/src/infrastructure/rabbitmq"
)

// IHealthService kiểm tra liveness/readiness của các thành phần phụ thuộc.
type IHealthService interface {
	Check(ctx context.Context) dtos.Result[*dtos.HealthResponse]
}

type healthService struct {
	cfg    config.IConfig
	db     postgres.IDB
	storage storage.IStorage
	rabbit rabbitmq.IConnection
}

func NewHealthService(cfg config.IConfig, db postgres.IDB, st storage.IStorage, rb rabbitmq.IConnection) IHealthService {
	return &healthService{cfg: cfg, db: db, storage: st, rabbit: rb}
}

func (s *healthService) Check(ctx context.Context) dtos.Result[*dtos.HealthResponse] {
	comps := map[string]string{}
	healthy := true
	mark := func(name string, err error) {
		if err != nil {
			comps[name] = "down: " + err.Error()
			healthy = false
		} else {
			comps[name] = "ok"
		}
	}

	// Postgres
	if err := s.db.GetDB().WithContext(ctx).Exec("SELECT 1").Error; err != nil {
		mark("postgres", err)
	} else {
		mark("postgres", nil)
	}

	// Qdrant (REST healthz)
	q := s.cfg.GetQdrant()
	qurl := fmt.Sprintf("%s://%s:%d/healthz", q.Scheme, q.Host, q.Port)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, qurl, nil)
	if q.Token != "" {
		req.Header.Set("api-key", q.Token)
	}
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		mark("qdrant", err)
	} else {
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			mark("qdrant", fmt.Errorf("http %d", resp.StatusCode))
		} else {
			mark("qdrant", nil)
		}
	}

	// SeaweedFS (HeadBucket qua Exists trên key không tồn tại)
	if _, err := s.storage.Exists(ctx, "__health__"); err != nil {
		mark("seaweedfs", err)
	} else {
		mark("seaweedfs", nil)
	}

	// RabbitMQ
	if ch, err := s.rabbit.OpenChannel(); err != nil {
		mark("rabbitmq", err)
	} else {
		_ = ch.Close()
		mark("rabbitmq", nil)
	}

	status := "ok"
	if !healthy {
		status = "degraded"
	}
	return dtos.Ok(&dtos.HealthResponse{Status: status, Components: comps})
}