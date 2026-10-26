package seaweedfs

import (
	"strings"
	"testing"
)

// TestBuildContentDispositionVietnamese là bằng chứng cho phần dễ sai nhất của
// chữ ký tải file: tên tiếng Việt phải đi được qua header HTTP thuần ASCII mà
// browser vẫn đọc lại đúng dấu. Thiếu một trong hai cái là user tải về file tên
// "ba-c-o.pdf" hoặc header sai cú pháp.
func TestBuildContentDispositionVietnamese(t *testing.T) {
	got := buildContentDisposition("Báo cáo nội bộ.pdf")
	if !strings.HasPrefix(got, `attachment; filename="`) {
		t.Errorf("phải là attachment kèm bản ASCII, nhận %q", got)
	}
	// Mỗi ký tự ngoài ASCII biến thành %XX theo từng byte UTF-8:
	// "Báo" = B + %C3%A1 + o, "ộ" = %E1%BB%99.
	wantExt := "filename*=UTF-8''B%C3%A1o%20c%C3%A1o%20n%E1%BB%99i%20b%E1%BB%99.pdf"
	if !strings.HasSuffix(got, wantExt) {
		t.Errorf("filename* phải percent-encode UTF-8, nhận %q", got)
	}
	// Cả hai phần của header phải thuần ASCII — con dấu sống sót ở đây là
	// header hỏng byte khi ghi ra dây.
	for i := 0; i < len(got); i++ {
		if got[i] > 0x7F {
			t.Fatalf("byte %d không ASCII (%#x) trong header %q", i, got[i], got)
		}
	}
}

func TestBuildContentDispositionASCIIOnly(t *testing.T) {
	got := buildContentDisposition("report-2026.pdf")
	want := `attachment; filename="report-2026.pdf"; filename*=UTF-8''report-2026.pdf`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Tên có nháy/backslash phải bị thoát (hoặc loại) để không phá vỡ quoted-string;
// tên thuần dấu không còn ký tự ASCII nào thì fallback "download".
func TestBuildContentDispositionUnsafeNames(t *testing.T) {
	if got := buildContentDisposition(`a"b\c.pdf`); !strings.Contains(got, `filename="`) ||
		strings.Count(got, `"`) != 2 {
		t.Errorf(`trích dẫn hỏng vì nháy/backslash: %q`, got)
	}
	got := buildContentDisposition("ú ơ")
	if !strings.HasPrefix(got, `attachment; filename="download"; `) {
		t.Errorf("tên thuần dấu phải fallback ASCII, nhận %q", got)
	}
	if !strings.HasSuffix(got, "filename*=UTF-8''%C3%BA%20%C6%A1") {
		t.Errorf("ext-value sai: %q", got)
	}
}
