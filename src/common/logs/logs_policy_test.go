package logs

import (
	"errors"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// useObserver tráo logger cấp package bằng logger ghi ra bộ nhớ để test đọc
// được field đúng thứ tự sau khi đi qua wrapper. Mặc định logger là nop (không
// LoadLogger) nên test vốn im lặng — ở đây test là nơi duy nhất bật logger.
func useObserver(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	core, seen := observer.New(zapcore.DebugLevel)
	lg := zap.New(core).Sugar()
	prev, prevCall := logger, callLogger
	logger = lg
	callLogger = lg.WithOptions(zap.AddCallerSkip(callerSkip))
	t.Cleanup(func() { logger, callLogger = prev, prevCall })
	return seen
}

// loggedKeys trả tên field theo đúng thứ tự zap nhận — cách đọc gần nhất với
// những gì in ra console vì encoder giữ nguyên vị trí field.
func loggedKeys(t *testing.T, seen *observer.ObservedLogs) []string {
	t.Helper()
	entries := seen.All()
	if len(entries) != 1 {
		t.Fatalf("số dòng log = %d, muốn 1", len(entries))
	}
	fields := entries[0].Context
	keys := make([]string, 0, len(fields))
	for _, f := range fields {
		keys = append(keys, f.Key)
	}
	return keys
}

// TestWrappersApplyPolicy: mọi ngả đi xuống zap (Infow, Warnw, Debugw, Error
// cùng hai bản *Skip) phải qua normalizeFields — chứng minh chính sách nằm ở
// tầng chung chứ không phải do từng call site tự xếp.
func TestWrappersApplyPolicy(t *testing.T) {
	cases := []struct {
		name string
		log  func()
		want []string
	}{
		{
			name: "Infow",
			log: func() {
				Infow("article: ký URL tải file", "file", "a.pdf", "trace_id", "t-1",
					"article_id", int64(69), "user_id", int64(7))
			},
			want: []string{"trace_id", "article_id", "user_id", "file"},
		},
		{
			name: "Warnw",
			log: func() {
				Warnw("chat: answer rỗng, retry", "message_chars", 12, "model", "qwen")
			},
			want: []string{"message_chars", "model"},
		},
		{
			name: "Debugw",
			log: func() {
				Debugw("debug", "zzz", 1, "aaa", 2)
			},
			want: []string{"aaa", "zzz"},
		},
		{
			name: "Error xuống cuối dù err được mồi đầu tiên",
			log: func() {
				Error(errors.New("boom"), "index: không đánh dấu done được",
					"article_id", int64(1), "trace_id", "t", "name", "quy chế.pdf")
			},
			want: []string{"trace_id", "article_id", "name", "error"},
		},
		{
			name: "InfowSkip (đường logger gorm)",
			log: func() {
				InfowSkip(0, "SQL record not found", "sql", "SELECT 1", "trace_id", "t", "rows", int64(0))
			},
			want: []string{"trace_id", "rows", "sql"},
		},
		{
			name: "WarnwSkip",
			log: func() {
				WarnwSkip(0, "SLOW SQL", "elapsed", "300ms", "trace_id", "t")
			},
			want: []string{"trace_id", "elapsed"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen := useObserver(t)
			tc.log()
			got := loggedKeys(t, seen)
			if !equalStrings(got, tc.want) {
				t.Fatalf("thứ tự field xuống zap = %v, muốn %v", got, tc.want)
			}
		})
	}
}

// TestWrapperCatGiaTriDai: chính sách cắt giá trị có tác dụng trên đường thật,
// không chỉ trong hàm thuần — SQL dài đi qua InfowSkip phải còn gọn.
func TestWrapperCatGiaTriDai(t *testing.T) {
	seen := useObserver(t)
	InfowSkip(0, "SLOW SQL", "sql", strings.Repeat("đ", maxLogValueRunes+7))
	fields := seen.All()[0].Context
	if fields[0].Key != "sql" {
		t.Fatalf("field đầu = %s, muốn sql", fields[0].Key)
	}
	if !strings.HasSuffix(fields[0].String, "ký tự]") {
		t.Fatalf("giá trị không bị cắt: %q", fields[0].String)
	}
	if n := len([]rune(fields[0].String)); n > maxLogValueRunes+20 {
		t.Fatalf("chuỗi sau cắt vẫn %d rune", n)
	}
}

// TestWrapperAnQueryURL: URL presigned lọt vào field nào cũng bị rút gọn, kể
// cả khi người viết log không nghĩ tới chuyện đó.
func TestWrapperAnQueryURL(t *testing.T) {
	seen := useObserver(t)
	Infow("file: ký URL upload", "upload_url",
		"http://localhost:8333/app-uploads/uploads/a.pdf?X-Amz-Signature=deadbeef")
	fields := seen.All()[0].Context
	if got := fields[0].String; strings.Contains(got, "X-Amz-Signature") {
		t.Fatalf("chữ ký còn trong log: %q", got)
	}
}

// TestLogPositionalKhongBiDoiXuong: các hàm không có field (Info/Warn/Errorf)
// đi đường riêng của zap, normalizeFields không được chen vào làm hỏng câu
// ghép sẵn.
func TestLogPositionalKhongBiDoiXuong(t *testing.T) {
	seen := useObserver(t)
	Warn("a", "b")
	if entries := seen.All(); len(entries) != 1 || entries[0].Message != "ab" {
		t.Fatalf("log positional bị đổi: %+v", entries)
	}
}
