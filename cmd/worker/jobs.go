package main

import (
	"context"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"time"

	"go.uber.org/fx"
)

// jobRunTimeout trần mỗi lượt chạy 1 job — quá thì hủy để kỳ sau làm lại
// (janitor retry hội tụ nên hủy giữa chừng không mất gì).
const jobRunTimeout = time.Minute

// Schedule khai báo 1 tác vụ nền: tên + nhịp chạy + hàm chạy + trần thời gian
// mỗi lượt. Thêm job mới = append 1 dòng Schedule lúc dựng Runner trong
// main.go, không cần interface hay struct adapter riêng. Timeout 0 thì dùng
// jobRunTimeout — job nặng (chunk/embed file 500 trang) phải set riêng, nếu
// không kỳ nào cũng bị cancel giữa chừng rồi làm lại từ đầu (đói vĩnh viễn).
type Schedule struct {
	Name     string
	Interval time.Duration
	Timeout  time.Duration
	Run      func(ctx context.Context) error
}

// asJob bọc method kiểu (int, error) — như PurgeDeleted trả số row đã dọn —
// thành dạng Run của Schedule (chỉ cần error).
func asJob(fn func(ctx context.Context) (int, error)) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		_, err := fn(ctx)
		return err
	}
}

// Runner giữ danh sách schedule và gắn vòng chạy vào fx lifecycle — mỗi
// schedule 1 goroutine + ticker riêng, job nặng không chặn job nhẹ.
type Runner struct {
	schedules []Schedule
}

func NewRunner(schedules []Schedule) *Runner {
	return &Runner{schedules: schedules}
}

// Attach đăng ký start/stop vào lifecycle: boot chạy mỗi job 1 phát ngay
// (khỏi chờ đủ kỳ đầu), stop hủy ctx để các loop thoát.
func (r *Runner) Attach(lc fx.Lifecycle) {
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			for _, s := range r.schedules {
				go r.loop(ctx, s)
			}
			return nil
		},
		OnStop: func(context.Context) error {
			cancel()
			return nil
		},
	})
}

func (r *Runner) loop(ctx context.Context, s Schedule) {
	r.runOnce(ctx, s)

	ticker := time.NewTicker(s.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runOnce(ctx, s)
		}
	}
}

// jobTimeout lấy trần mỗi lượt chạy — job nào không set thì dùng mặc định.
func jobTimeout(s Schedule) time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	return jobRunTimeout
}

func (r *Runner) runOnce(ctx context.Context, s Schedule) {
	// Runner (middleware) sinh trace_id 1 lần mỗi lượt chạy rồi nhét vào
	// ctx — service tầng dưới chỉ đọc ra gắn log, không tự sinh. Cùng 1 id
	// phủ từ log runner tới log service trong cả lượt chạy.
	traceID, err := utils.NewUUIDv7()
	if err != nil {
		logs.Error(err, "worker: không sinh được trace_id, bỏ kỳ này", "job", s.Name)
		return
	}
	runCtx, cancel := context.WithTimeout(ctx, jobTimeout(s))
	defer cancel()
	runCtx = utils.ContextWithTraceID(runCtx, traceID)
	if err := s.Run(runCtx); err != nil {
		logs.Error(err, "worker: job lỗi, kỳ sau thử lại", "job", s.Name, "trace_id", traceID)
		return
	}
	logs.Infow("worker: job xong", "job", s.Name, "trace_id", traceID)
}
