package chunker

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitShortTextSingleChunk(t *testing.T) {
	got := Split("xin chào thế giới", Option{})
	if len(got) != 1 || got[0] != "xin chào thế giới" {
		t.Fatalf("got %q", got)
	}
}

func TestSplitEmpty(t *testing.T) {
	if got := Split("   ", Option{}); len(got) != 0 {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestSplitRespectsChunkSize(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString("đoạn văn bản tiếng Việt cần chia nhỏ để embedding. ")
	}
	got := Split(b.String(), Option{ChunkSize: 200, Overlap: 20})
	if len(got) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(got))
	}
	for i, ch := range got {
		if n := utf8.RuneCountInString(ch); n > 200 {
			t.Fatalf("chunk %d quá dài: %d rune", i, n)
		}
		if !utf8.ValidString(ch) {
			t.Fatalf("chunk %d vỡ UTF-8", i)
		}
	}
}

func TestSplitKeepsParagraphBoundary(t *testing.T) {
	p1 := strings.Repeat("a ", 60) // ~120 ký tự
	p2 := strings.Repeat("b ", 60)
	got := Split(p1+"\n\n"+p2, Option{ChunkSize: 150, Overlap: 0})
	if len(got) < 2 {
		t.Fatalf("expected split theo đoạn văn, got %d chunk", len(got))
	}
	for _, ch := range got {
		if strings.Contains(ch, "a") && strings.Contains(ch, "b") {
			t.Fatalf("chunk trộn 2 đoạn văn: %q", ch)
		}
	}
}

func TestSplitOverlapCarriesContext(t *testing.T) {
	text := strings.Repeat("từ ", 100) // 300 ký tự
	got := Split(text, Option{ChunkSize: 100, Overlap: 10})
	if len(got) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(got))
	}
	// chunk sau phải bắt đầu bằng đuôi chunk trước (overlap từ)
	tail := lastRunes(strings.TrimSpace(got[0]), 10)
	if !strings.HasPrefix(strings.TrimSpace(got[1]), strings.TrimSpace(tail)) {
		t.Fatalf("thiếu overlap: %q vs %q", got[0], got[1])
	}
}

func TestSplitLongWordNoSeparator(t *testing.T) {
	text := strings.Repeat("x", 500) // không dấu cắt nào ngoài ""
	got := Split(text, Option{ChunkSize: 100, Overlap: 0})
	if len(got) != 5 {
		t.Fatalf("expected 5 chunk, got %d", len(got))
	}
}
