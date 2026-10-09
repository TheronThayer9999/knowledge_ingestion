package worker

// Tiện ích dùng chung cho mọi worker service (chunk/embed/purge) — gom về 1
// package internal để service HTTP cùng package cha không thấy, khỏi lẫn lộn
// helper worker với nghiệp vụ API. Không chứa logic nghiệp vụ nên không cần
// mock. Chỉ import được trong cây services (luật internal của Go).

import (
	"context"
	"time"

	"knowledge_ingestion/src/common/utils"
)

// Detached là ctx đã tách khỏi cancel của runner — mọi điểm ghi DB (done,
// retry, dọn dẹp) xài kiểu này để ghi được dù job hết timeout. Vòng đời
// cancel quản lý bằng method Close: caller chỉ `defer d.Close()`, không cầm
// CancelFunc rời nên không thể quên (leak timer).
type Detached struct {
	Ctx context.Context
	cancel context.CancelFunc
}

// Detach tách ctx khỏi cancel của runner, TTL 30s — đủ cho vài câu UPDATE/
// DELETE đánh dấu, không phải chỗ chạy nghiệp vụ nặng.
func Detach(ctx context.Context) Detached {
	c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	return Detached{Ctx: c, cancel: cancel}
}

// Close đóng vòng đời ctx đã tách — luôn defer ngay sau Detach.
func (d Detached) Close() {
	d.cancel()
}

// EnsureTraceID đọc trace_id runner đã gắn vào ctx; gọi trực tiếp (test,
// tool tay) không có thì tự sinh fallback để log vẫn lần được. Giữ nguyên
// function vì không có vòng đời gì để quản lý.
func EnsureTraceID(ctx context.Context) (string, error) {
	if traceID := utils.TraceIDFromCtx(ctx); traceID != "" {
		return traceID, nil
	}
	return utils.NewUUIDv7()
}

// WithRetry chạy fn tối đa attempts lần, nghỉ backoff nhân đôi giữa các lần,
// tôn trọng ctx hủy — dùng cho ollama/qdrant chập chờn mạng. Lỗi cuối cùng
// trả về để service đánh MarkEmbedError theo backoff queue. Giữ nguyên
// function vì vòng đời thuộc về ctx của caller, không có gì để giữ.
func WithRetry(ctx context.Context, attempts int, base time.Duration, fn func(ctx context.Context) error) error {
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
