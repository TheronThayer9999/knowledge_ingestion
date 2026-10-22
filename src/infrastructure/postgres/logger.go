package postgres

import (
	"context"
	stderrors "errors"
	"runtime"
	"time"

	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	gormutils "gorm.io/gorm/utils"
)

// slowSQLThreshold ngưỡng log query chậm — giữ đúng 200ms của gorm default
// trước đây để không đổi hành vi cảnh báo, chỉ thêm trace_id.
const slowSQLThreshold = 200 * time.Millisecond

// traceLogger thay gorm default logger để dòng SLOW SQL nào cũng kèm
// trace_id của request/job gây ra nó (đọc từ ctx — controller truyền `c`
// thẳng xuống repo nên trace của middleware tới được đây). Không có trace
// trong ctx thì field rỗng, log vẫn ra bình thường.
//
// Caller: gorm gọi Trace từ bên trong thư viện nên zap mặc định chỉ lần
// được tới file này. callerSkipToRepo đếm frame thực tế để dòng log trỏ
// đúng file:line của repository đã phát sinh query.
type traceLogger struct{}

// callerSkipToRepo đếm số frame từ Trace lên tới target — caller do chính
// gorm xác định qua utils.CallerFrame (nó bỏ frame nội bộ của mình), nên
// không hard-code package nào. target phải được CallerFrame() gọi NGAY trong
// Trace, thêm một lớp helper là gorm dừng nhầm ở chính Trace. Không tìm
// thấy thì trả 0 để log vẫn ra.
//
//go:noinline
func callerSkipToRepo(target runtime.Frame) int {
	if target.PC == 0 {
		return 0
	}
	pcs := make([]uintptr, 32)
	n := runtime.Callers(2, pcs) // bỏ runtime.Callers + callerSkipToRepo
	frames := runtime.CallersFrames(pcs[:n])
	for skip := 0; ; skip++ {
		frame, more := frames.Next()
		if frame.File == target.File && frame.Line == target.Line {
			return skip
		}
		if !more {
			return 0
		}
	}
}

func (traceLogger) LogMode(gormlogger.LogLevel) gormlogger.Interface { return traceLogger{} }

func (traceLogger) Info(context.Context, string, ...any) {}

func (traceLogger) Warn(ctx context.Context, msg string, data ...any) {
	logs.Warnw(msg, append([]any{"trace_id", utils.TraceIDFromCtx(ctx)}, data...)...)
}

func (traceLogger) Error(ctx context.Context, msg string, data ...any) {
	logs.Errorf(msg+" [trace_id=%s]", append(data, utils.TraceIDFromCtx(ctx))...)
}

func (traceLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)
	sql, rows := fc()
	fields := []any{"trace_id", utils.TraceIDFromCtx(ctx), "elapsed", elapsed.String(), "rows", rows, "sql", sql}
	switch {
	case err != nil:
		// record not found là luồng thường (404 do service map), không phải
		// lỗi SQL — log info cho biết, cấm in stack ERROR gây hoảng.
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			logs.InfowSkip(callerSkipToRepo(gormutils.CallerFrame()), "SQL record not found", fields...)
			return
		}
		logs.ErrorSkip(callerSkipToRepo(gormutils.CallerFrame()), err, "SQL error", fields...)
	case elapsed > slowSQLThreshold:
		logs.WarnwSkip(callerSkipToRepo(gormutils.CallerFrame()), "SLOW SQL", fields...)
	}
}
