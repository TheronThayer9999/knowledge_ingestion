package logs

import (
	"os"
	"sync"

	"go.uber.org/zap"
)

var (
	once   sync.Once
	logger = zap.NewNop().Sugar()
)

func LoadLogger() {
	once.Do(func() {
		cfg := zap.NewDevelopmentConfig()
		if level := os.Getenv("LOG_LEVEL"); level != "" {
			_ = cfg.Level.UnmarshalText([]byte(level))
		}
		if l, err := cfg.Build(); err == nil {
			logger = l.Sugar()
		}
	})
}

func Debug(args ...interface{}) {
	logger.Debug(args...)
}

func Info(args ...interface{}) {
	logger.Info(args...)
}

func Infof(template string, args ...interface{}) {
	logger.Infof(template, args...)
}

func Infow(msg string, kv ...interface{}) {
	logger.Infow(msg, kv...)
}

func Warn(args ...interface{}) {
	logger.Warn(args...)
}

func Warnf(template string, args ...interface{}) {
	logger.Warnf(template, args...)
}

func Warnw(msg string, kv ...interface{}) {
	logger.Warnw(msg, kv...)
}

func Error(err error, msg string, kv ...interface{}) {
	logger.Errorw(msg, append([]interface{}{"error", err}, kv...)...)
}

func Errorf(template string, args ...interface{}) {
	logger.Errorf(template, args...)
}

func Fatal(args ...interface{}) {
	logger.Fatal(args...)
}

func Fatalf(template string, args ...interface{}) {
	logger.Fatalf(template, args...)
}

func Sync() {
	_ = logger.Sync()
}
