package logs

import (
	"bytes"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// WARN không được kèm stack trace (SLOW SQL từng bị đọc như panic runtime),
// ERROR thì vẫn phải có. Test khoá đúng bộ option mà newLogger dùng
// (stackOptions) — console encoder in stack thành các dòng nối sau entry nên
// đếm khoảng cách dòng thay vì tìm field "stacktrace".
func TestStackOptionsOnlyFromError(t *testing.T) {
	cfg := zap.NewDevelopmentConfig()
	buf := &bytes.Buffer{}
	logger := zap.New(
		zapcore.NewCore(
			zapcore.NewConsoleEncoder(cfg.EncoderConfig),
			zapcore.AddSync(buf),
			zap.DebugLevel,
		),
		stackOptions()...,
	)
	logger.Warn("SLOW SQL", zap.Int64("elapsed_ms", 298))
	logger.Error("lỗi thật", zap.Int64("article_id", 54))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	warnAt := lineOf(lines, "SLOW SQL")
	errAt := lineOf(lines, "lỗi thật")
	if warnAt < 0 || errAt < 0 {
		t.Fatalf("thiếu dòng log: %q", lines)
	}
	if errAt != warnAt+1 {
		t.Errorf("WARN bị chèn %d dòng stack, muốn 0: %q", errAt-warnAt-1, lines)
	}
	if len(lines) <= errAt+1 {
		t.Errorf("ERROR thiếu stack trace: %q", lines)
	}
}

func lineOf(lines []string, needle string) int {
	for i, line := range lines {
		if strings.Contains(line, needle) {
			return i
		}
	}
	return -1
}
