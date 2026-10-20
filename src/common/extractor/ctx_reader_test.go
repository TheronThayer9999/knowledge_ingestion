package extractor

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestCtxReaderStopsOnCancel khoá hành vi dừng đọc ngay khi ctx hủy — nhờ đó
// io.ReadAll/io.Copy trên blob lớn (docx, spool PDF) không đọc hết file sau
// khi job đã bị cancel.
func TestCtxReaderStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := &ctxReader{ctx: ctx, r: strings.NewReader(strings.Repeat("x", 1<<20))}
	if _, err := io.ReadAll(r); !errors.Is(err, context.Canceled) {
		t.Fatalf("muốn context.Canceled, nhận %v", err)
	}
}
