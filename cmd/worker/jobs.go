package main

import (
	"context"
	"fmt"
	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"runtime/debug"
	"sync"
	"time"

	"go.uber.org/fx"
)

// jobRunTimeout trần mỗi lượt chạy 1 job — quá thì hủy để kỳ sau làm lại
// (janitor retry hội tụ nên hủy giữa chừng không mất gì).
const jobRunTimeout = time.Minute

// Schedule khai báo 1 tác vụ nền: tên + nhịp chạy + hàm chạy + trần thời gian
// mỗi lượt. Thêm job mới = append 1 dòng Schedule lúc dựng Runner trong
// main.go. Timeout 0 thì dùng jobRunTimeout — job nặng (chunk/embed file 500
// trang) phải set riêng, nếu không kỳ nào cũng bị cancel giữa chừng rồi làm
// lại từ đầu (đói vĩnh viễn). Run chỉ cần error vì số lượng đã nằm trong log
// service, mỗi job khai closure tường minh 3 dòng thay vì adapter chung.
type Schedule struct {
	Name     string
	Interval time.Duration
	Timeout  time.Duration
	Run      func(ctx context.Context) error
}

// Runner giữ danh sách schedule và gắn vòng chạy vào fx lifecycle — mỗi
// schedule 1 goroutine + ticker riêng, job nặng không chặn job nhẹ.
type Runner struct {
	schedules []Schedule
	// wg đếm vòng lặp của từng schedule để OnStop chờ job đang chạy thoát
	// trước khi fx đóng DB/Qdrant — nếu không, goroutine còn ghi trong lúc
	// tài nguyên đã đóng.
	wg sync.WaitGroup
}

func NewRunner(schedules []Schedule) *Runner {
	return &Runner{schedules: schedules}
}

// Attach đăng ký start/stop vào lifecycle: boot chạy mỗi job 1 phát ngay
// (khỏi chờ đủ kỳ đầu), stop hủy ctx và chờ các vòng lặp thoát (có giới hạn).
func (r *Runner) Attach(lc fx.Lifecycle) {
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			for _, s := range r.schedules {
				r.wg.Add(1)
				go func(s Schedule) {
					defer r.wg.Done()
					r.loop(ctx, s)
				}(s)
			}
			return nil
		},
		OnStop: func(context.Context) error {
			cancel()
			done := make(chan struct{})
			go func() {
				r.wg.Wait()
				close(done)
			}()
			select {
			case <-done:
				logs.Info("worker: mọi job đã dừng trước khi đóng tài nguyên")
			case <-time.After(constants.SHUTDOWN_GRACE):
				// Job nặng chưa kịp thoát: vẫn trả về để process không treo.
				// Service đã được cancel và tự ghi trạng thái bằng detached ctx
				// ở batch gần nhất, nên rủi ro mất dữ liệu là tối thiểu.
				logs.Warnw("worker: job chưa dừng hết sau grace, vẫn đóng tài nguyên",
					"grace", constants.SHUTDOWN_GRACE)
			}
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

func (r *Runner) runOnce(ctx context.Context, s Schedule) {
	// Goroutine của schedule không có gin.Recovery — panic ở đây giết cả
	// process. Bắt lại để kỳ sau chạy tiếp thay vì sập worker.
	defer func() {
		if rec := recover(); rec != nil {
			logs.Error(fmt.Errorf("panic: %v", rec), "worker: job panic, bỏ kỳ này",
				"job", s.Name, "stack", string(debug.Stack()))
		}
	}()

	// Runner (middleware) sinh trace_id 1 lần mỗi lượt chạy rồi nhét vào
	// ctx — service tầng dưới chỉ đọc ra gắn log, không tự sinh. Cùng 1 id
	// phủ từ log runner tới log service trong cả lượt chạy.
	traceID, err := utils.NewUUIDv7()
	if err != nil {
		logs.Error(err, "worker: không sinh được trace_id, bỏ kỳ này", "job", s.Name)
		return
	}
	// Timeout 0 thì dùng mặc định — job nặng phải set riêng, nếu không kỳ nào
	// cũng bị cancel giữa chừng rồi làm lại từ đầu (đói vĩnh viễn).
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = jobRunTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	runCtx = utils.ContextWithTraceID(runCtx, traceID)
	if err := s.Run(runCtx); err != nil {
		logs.Error(err, "worker: job lỗi, kỳ sau thử lại", "job", s.Name, "trace_id", traceID)
		return
	}
	logs.Infow("worker: job xong", "job", s.Name, "trace_id", traceID)
}
