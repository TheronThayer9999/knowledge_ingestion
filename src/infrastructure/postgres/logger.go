package postgres

import (
	"context"
	stderrors "errors"
	"time"

	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// slowSQLThreshold ngưỡng log query chậm — giữ đúng 200ms của gorm default
// trước đây để không đổi hành vi cảnh báo, chỉ thêm trace_id.
const slowSQLThreshold = 200 * time.Millisecond

// traceLogger thay gorm default logger để dòng SLOW SQL nào cũng kèm
// trace_id của request/job gây ra nó (đọc từ ctx — controller truyền `c`
// thẳng xuống repo nên trace của middleware tới được đây). Không có trace
// trong ctx thì field rỗng, log vẫn ra bình thường.
type traceLogger struct{}

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
			logs.Infow("SQL record not found", fields...)
			return
		}
		logs.Error(err, "SQL error", fields...)
	case elapsed > slowSQLThreshold:
		logs.Warnw("SLOW SQL", fields...)
	}
}
