package services

import (
	"context"
	"fmt"
	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/domain"
	"os"
	"time"
)

// IWorkerHeartbeatService giữ nhịp sống của tiến trình worker trong DB để
// API trả cho UI biết worker còn chạy hay không.
type IWorkerHeartbeatService interface {
	// Run ghi nhịp đầu tiên rồi lặp mỗi WORKER_HEARTBEAT_INTERVAL cho tới khi
	// ctx bị hủy (trả về khi đã dọn row của mình).
	Run(ctx context.Context)
}

type workerHeartbeatService struct {
	repo     domain.IWorkerHeartbeatRepository
	workerID string
}

// NewWorkerHeartbeatService worker_id = host:pid: restart worker là pid mới
// nên row cũ của tiến trình trước không bị "thừa hưởng" nhịp sống.
func NewWorkerHeartbeatService(repo domain.IWorkerHeartbeatRepository) IWorkerHeartbeatService {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	return &workerHeartbeatService{
		repo:     repo,
		workerID: fmt.Sprintf("%s:%d", host, os.Getpid()),
	}
}

func (s *workerHeartbeatService) Run(ctx context.Context) {
	started := time.Now()
	s.beat(ctx, started)

	ticker := time.NewTicker(constants.WORKER_HEARTBEAT_INTERVAL)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.forget()
			return
		case <-ticker.C:
			s.beat(ctx, started)
		}
	}
}

func (s *workerHeartbeatService) beat(ctx context.Context, started time.Time) {
	// Heartbeat lỗi không được giết job: chỉ mất tín hiệu trên UI, worker vẫn
	// phải chạy tiếp.
	if err := s.repo.Beat(ctx, s.workerID, started); err != nil {
		logs.Warnw("worker: ghi heartbeat lỗi", "worker_id", s.workerID, "error", err)
	}
}

// forget xoá row khi tắt sạch — ctx lúc này đã bị hủy nên phải dùng ctx tách,
// không thì lệnh xoá bị hủy trước khi kịp chạy.
func (s *workerHeartbeatService) forget() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.repo.Forget(ctx, s.workerID); err != nil {
		logs.Warnw("worker: xoá heartbeat lỗi, UI sẽ thấy offline chậm", "worker_id", s.workerID, "error", err)
	}
}
