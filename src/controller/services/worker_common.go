package services

// Tiện ích dùng chung cho mọi worker (chunk/embed/purge) — gom về 1 chỗ thay
// vì copy mỗi service 1 bản: tách ctx khỏi cancel của runner, retry backoff,
// trace_id fallback. Không chứa logic nghiệp vụ nên không cần mock.

import (
	"context"
	"time"

	"knowledge_ingestion/src/common/utils"
)

// detachCtx tách ctx khỏi cancel của runner để đánh dấu DB (done/retry) khi
// job hết timeout — trạng thái queue phải ghi được dù lượt chạy bị hủy.
func detachCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
}

// ensureTraceID đọc trace_id runner đã gắn vào ctx; gọi trực tiếp (test,
// tool tay) không có thì tự sinh fallback để log vẫn lần được.
func ensureTraceID(ctx context.Context) (string, error) {
	if traceID := utils.TraceIDFromCtx(ctx); traceID != "" {
		return traceID, nil
	}
	return utils.NewUUIDv7()
}

// withRetry chạy fn tối đa attempts lần, nghỉ backoff nhân đôi giữa các lần,
// tôn trọng ctx hủy — dùng cho ollama/qdrant chập chờn mạng. Lỗi cuối cùng
// trả về để service đánh MarkEmbedError theo backoff queue.
func withRetry(ctx context.Context, attempts int, base time.Duration, fn func(ctx context.Context) error) error {
	var err error
	delay := base
	for i := 0; i < attempts; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = fn(ctx); err == nil {
			return nil
		}
		if i == attempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			delay *= 2
		}
	}
	return err
}
