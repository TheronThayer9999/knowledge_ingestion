package logs

import (
	"os"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// callerSkip bù đúng 1 frame của wrapper trong package này. zap ghi caller
// tại hàm gọi trực tiếp `logger.Xxx`; không skip thì mọi dòng log đều trỏ
// về logs.go thay vì file thật sự gọi logs.Infow. Mỗi wrapper dưới đây gọi
// thẳng logger đúng 1 lần nên hằng số 1 là đủ — wrapper lồng nhau phải tăng.
const callerSkip = 1

var (
	once   sync.Once
	logger = zap.NewNop().Sugar()
	// callLogger = logger đã bù frame wrapper, dùng cho MỌI hàm export.
	callLogger = logger.WithOptions(zap.AddCallerSkip(callerSkip))
)

func LoadLogger() {
	once.Do(func() {
		cfg := zap.NewDevelopmentConfig()
		// Development mặc định gắn stack từ WARN (Config.buildOptions đặt
		// stackLevel = WarnLevel) nên SLOW SQL / backoff trông y như panic
		// runtime. Tắt của config rồi bật lại đúng ở ErrorLevel trong newLogger.
		cfg.DisableStacktrace = true
		if level := os.Getenv("LOG_LEVEL"); level != "" {
			_ = cfg.Level.UnmarshalText([]byte(level))
		}
		if l, err := newLogger(cfg); err == nil {
			logger = l.Sugar()
			callLogger = logger.WithOptions(zap.AddCallerSkip(callerSkip))
		}
	})
}

// newLogger dựng logger từ cfg với đúng một ngoại lệ của dự án: stack trace chỉ
// gắn từ ERROR. Config.Build áp tham số opts SAU buildOptions nên AddStacktrace
// ở đây thắng mức mặc định của Development.
func newLogger(cfg zap.Config) (*zap.Logger, error) {
	return cfg.Build(stackOptions()...)
}

// stackOptions là nguồn duy nhất của chính sách stack trace (test khoá đúng hàm
// này): WARN sạch sẽ, ERROR mới kèm stack.
func stackOptions() []zap.Option {
	return []zap.Option{zap.AddStacktrace(zapcore.ErrorLevel)}
}

func Debug(args ...interface{}) {
	callLogger.Debug(args...)
}

func Debugw(msg string, kv ...interface{}) {
	callLogger.Debugw(msg, kv...)
}

func Info(args ...interface{}) {
	callLogger.Info(args...)
}

func Infof(template string, args ...interface{}) {
	callLogger.Infof(template, args...)
}

func Infow(msg string, kv ...interface{}) {
	callLogger.Infow(msg, kv...)
}

// InfowSkip như Infow nhưng lùi thêm skip frame khi xác định caller — dùng cho
// logger nằm sâu trong thư viện (vd gorm logger): frame giữa wrapper và nơi
// phát sinh log không cố định nên caller chỉnh tay, để dòng log trỏ đúng về
// repository thay vì file wrapper. skip là số frame lùi thêm tính từ hàm gọi
// InfowSkip — 0 là đúng hàm đó.
//
//go:noinline
func InfowSkip(skip int, msg string, kv ...interface{}) {
	callLogger.WithOptions(zap.AddCallerSkip(skip)).Infow(msg, kv...)
}

func Warn(args ...interface{}) {
	callLogger.Warn(args...)
}

func Warnf(template string, args ...interface{}) {
	callLogger.Warnf(template, args...)
}

func Warnw(msg string, kv ...interface{}) {
	callLogger.Warnw(msg, kv...)
}

// WarnwSkip như Warnw nhưng lùi thêm skip frame — xem InfowSkip.
//
//go:noinline
func WarnwSkip(skip int, msg string, kv ...interface{}) {
	callLogger.WithOptions(zap.AddCallerSkip(skip)).Warnw(msg, kv...)
}

func Error(err error, msg string, kv ...interface{}) {
	callLogger.Errorw(msg, append([]interface{}{"error", err}, kv...)...)
}

// ErrorSkip như Error nhưng lùi thêm skip frame — xem InfowSkip.
//
//go:noinline
func ErrorSkip(skip int, err error, msg string, kv ...interface{}) {
	callLogger.WithOptions(zap.AddCallerSkip(skip)).Errorw(msg, append([]interface{}{"error", err}, kv...)...)
}

func Errorf(template string, args ...interface{}) {
	callLogger.Errorf(template, args...)
}

func Fatal(args ...interface{}) {
	callLogger.Fatal(args...)
}

func Fatalf(template string, args ...interface{}) {
	callLogger.Fatalf(template, args...)
}

func Sync() {
	_ = logger.Sync()
}
