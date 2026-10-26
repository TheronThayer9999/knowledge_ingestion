package logs

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"go.uber.org/zap/zapcore"
)

// fieldKeys trả tên field theo đúng thứ tự trong kv đã chuẩn hoá — cách đọc
// kết quả gần nhất với thứ tự in ra console (encoder giữ nguyên vị trí field).
func fieldKeys(kv []any) []string {
	keys := make([]string, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok {
			return nil
		}
		keys = append(keys, key)
	}
	return keys
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestNormalizeOrdersTier: trace_id đầu, *_id tiếp (A→Z), còn lại A→Z, error
// cuối — dù caller xếp lộn xộn tới đâu.
func TestNormalizeOrdersTier(t *testing.T) {
	cases := []struct {
		name string
		kv   []any
		want []string
	}{
		{
			name: "caller viết tùy hứng vẫn ra khuôn",
			kv: []any{
				"sql", "SELECT 1", "user_id", int64(7), "error", errors.New("boom"),
				"trace_id", "t-1", "model", "qwen", "article_id", int64(69), "id", int64(3),
			},
			want: []string{"trace_id", "article_id", "id", "user_id", "model", "sql", "error"},
		},
		{
			name: "chỉ field thường thì sắp A→Z",
			kv:   []any{"status", 200, "method", "GET", "latency", "1ms"},
			want: []string{"latency", "method", "status"},
		},
		{
			name: "nhiều id cùng tầng sắp theo tên",
			kv:   []any{"user_id", 1, "article_id", 2, "category_id", 3},
			want: []string{"article_id", "category_id", "user_id"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fieldKeys(normalizeFields(tc.kv))
			if !equalStrings(got, tc.want) {
				t.Fatalf("thứ tự field = %v, muốn %v", got, tc.want)
			}
		})
	}
}

// TestNormalizeIgnoresMalformed: kv lẻ hoặc key không phải chuỗi thì trả nguyên
// để zap tự in (!BADKEY/MISSING), không đoán bừa tên field.
func TestNormalizeIgnoresMalformed(t *testing.T) {
	t.Run("kv lẻ", func(t *testing.T) {
		kv := []any{"a", 1, "b"}
		got := normalizeFields(kv)
		if len(got) != 3 || got[2] != "b" {
			t.Fatalf("kv lẻ bị sửa: %v", got)
		}
	})
	t.Run("key không phải chuỗi", func(t *testing.T) {
		kv := []any{7, "value", "trace_id", "t"}
		got := normalizeFields(kv)
		if got[0] != 7 {
			t.Fatalf("key số bị sắp xếp lại thành %v — phải trả nguyên", got)
		}
	})
	t.Run("rỗng", func(t *testing.T) {
		if got := normalizeFields(nil); len(got) != 0 {
			t.Fatalf("kv rỗng trả %v", got)
		}
	})
}

// TestTruncateRunes: cắt theo rune (tiếng Việt không bị cắt giữa chữ) và marker
// cho biết còn bao nhiêu ký tự nữa.
func TestTruncateRunes(t *testing.T) {
	t.Run("ngắn hơn trần thì giữ nguyên", func(t *testing.T) {
		s := strings.Repeat("á", maxLogValueRunes)
		if got := truncateRunes(s); got != s {
			t.Fatalf("chuỗi đúng trần bị cắt: %d rune", len([]rune(got)))
		}
	})
	t.Run("dài hơn trần thì cắt và báo dư", func(t *testing.T) {
		s := strings.Repeat("ạ", maxLogValueRunes+100)
		got := truncateRunes(s)
		wantTail := fmt.Sprintf("…[+%d ký tự]", 100)
		if !strings.HasSuffix(got, wantTail) {
			t.Fatalf("đuôi = %q, muốn kết thúc bằng %q", got, wantTail)
		}
		head := strings.TrimSuffix(got, wantTail)
		if n := len([]rune(head)); n != maxLogValueRunes {
			t.Fatalf("đầu chuỗi còn %d rune, muốn %d", n, maxLogValueRunes)
		}
		if !strings.HasPrefix(head, "ạạạ") {
			t.Fatalf("đầu chuỗi không đúng nội dung: %q", head[:6])
		}
	})
	t.Run("byte không bị cắt giữa ký tự", func(t *testing.T) {
		got := truncateRunes(strings.Repeat("ế", maxLogValueRunes+50))
		for _, r := range got {
			if r == '\uFFFD' {
				t.Fatal("cắt trúng giữa ký tự UTF-8, sinh ký tự rác")
			}
		}
	})
}

// TestHideURLQuery: URL ký (presigned) không còn mang credential trong log.
func TestHideURLQuery(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "presigned GET",
			in:   "http://localhost:8333/app-uploads/uploads/a.pdf?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=deadbeef",
			want: "http://localhost:8333/app-uploads/uploads/a.pdf" + hiddenQueryNote,
		},
		{
			name: "fragment cũng ẩn",
			in:   "https://example.com/docs#section-2",
			want: "https://example.com/docs" + hiddenQueryNote,
		},
		{
			name: "URL không query thì nguyên văn",
			in:   "https://example.com/rules.pdf",
			want: "https://example.com/rules.pdf",
		},
		{
			name: "văn bản có chữ http giữa câu không bị đụng",
			in:   "tải ở http example.com/x?y=1 nhé",
			want: "tải ở http example.com/x?y=1 nhé",
		},
		{
			name: "SQL chứa link cũng không bị rút gọn",
			in:   "SELECT * FROM articles WHERE url = 'https://x.com/a?b=1'",
			want: "SELECT * FROM articles WHERE url = 'https://x.com/a?b=1'",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hideURLQuery(tc.in); got != tc.want {
				t.Fatalf("hideURLQuery = %q, muốn %q", got, tc.want)
			}
		})
	}
}

// TestSanitizeValueLoaiKhac: chỉ chuỗi/[]byte bị cắt; number, bool, error để
// nguyên (error phải giữ để zap encode ErrorType như cũ).
func TestSanitizeValueGiuNguyenLoaiKhac(t *testing.T) {
	err := errors.New("boom")
	if got := sanitizeValue(err); got != any(err) {
		t.Fatalf("error bị thay thành %T", got)
	}
	if got := sanitizeValue(int64(42)); got != any(int64(42)) {
		t.Fatalf("int64 bị thay: %v", got)
	}
	if got := sanitizeValue(zapcore.InfoLevel); got == nil {
		t.Fatal("field không phải chuỗi bị mất")
	}
	long := []byte(strings.Repeat("b", maxLogValueRunes+1))
	if got, ok := sanitizeValue(long).(string); !ok || !strings.HasSuffix(got, "ký tự]") {
		t.Fatalf("[]byte dài không bị cắt: %#v", got)
	}
}

// TestNormalizeDoesNotMutateCaller: helper không ghi đè slice của caller —
// Error() mồi thêm cặp "error", err vào slice mới tạo nhưng caller vẫn có thể
// dùng lại kv cũ an toàn.
func TestNormalizeKhongThayDoiSliceCaller(t *testing.T) {
	kv := []any{"b", 1, "a", 2}
	_ = normalizeFields(kv)
	if kv[0] != "b" || kv[2] != "a" {
		t.Fatalf("slice caller bị sắp lại: %v", kv)
	}
}
