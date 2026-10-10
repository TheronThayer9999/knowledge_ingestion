package chunker

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

// Recursive Eino phải cắt ở "…" (dấu câu văn Việt) và giữ chunk đúng cỡ rune.
func TestRecursiveSplitsAtEllipsis(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 20; i++ {
		b.WriteString("Khách hàng phản hồi tích cực về chất lượng dịch vụ… ")
	}
	c, err := NewRecursive(Option{ChunkSize: 100, Overlap: 10})
	if err != nil {
		t.Fatalf("NewRecursive: %v", err)
	}
	got, err := c.Split(context.Background(), b.String())
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(got) < 2 {
		t.Fatalf("expected nhiều chunk, got %d", len(got))
	}
	for i, ch := range got {
		if n := utf8.RuneCountInString(ch); n > 100 {
			t.Fatalf("chunk %d quá dài: %d rune", i, n)
		}
		if !utf8.ValidString(ch) {
			t.Fatalf("chunk %d vỡ UTF-8", i)
		}
	}
}

// Đo bằng rune chứ không phải byte: chunk đầy ~ChunkSize rune chữ có dấu thì
// số byte phải lớn hơn ChunkSize (mỗi ký tự 2–3 byte).
func TestRecursiveCountsRunesNotBytes(t *testing.T) {
	text := strings.Repeat("tiếng Việt có dấu ", 50) // mỗi từ ~6 rune, ~13 byte
	c, err := NewRecursive(Option{ChunkSize: 60, Overlap: 0})
	if err != nil {
		t.Fatalf("NewRecursive: %v", err)
	}
	got, err := c.Split(context.Background(), text)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(got) < 2 {
		t.Fatalf("expected nhiều chunk, got %d", len(got))
	}
	if n := utf8.RuneCountInString(got[0]); n > 60 {
		t.Fatalf("chunk đầu quá dài: %d rune", n)
	}
	if len(got[0]) <= 60 {
		t.Fatalf("chunk đo bằng byte, không phải rune: %d byte", len(got[0]))
	}
}

// fakeTopicEmbedder phân cụm nghĩa cứng cho test semantic: câu ẩm thực →
// vector A, câu xe cộ → vector B. Không gọi mạng.
type fakeTopicEmbedder struct{}

func topicOf(s string) []float32 {
	if strings.Contains(s, "Phở") || strings.Contains(s, "Bún") ||
		strings.Contains(s, "TS.") || strings.Contains(s, "Nguyễn Văn A") {
		return []float32{1, 0}
	}
	return []float32{0, 1}
}

func (fakeTopicEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	return topicOf(text), nil
}

func (fakeTopicEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, s := range texts {
		out[i] = topicOf(s)
	}
	return out, nil
}

func (fakeTopicEmbedder) Dimension() int                  { return 2 }
func (fakeTopicEmbedder) ModelName() string               { return "fake" }
func (fakeTopicEmbedder) IsPermanentError(err error) bool { return false }

// Semantic phải cắt theo cụm nghĩa (2 chunk), và viết tắt "TS." không được
// gây cắt nhầm — nếu chẻ ở dấu "." sau "TS" thì đã ra 3 chunk.
func TestSemanticGroupsByMeaningKeepsAbbreviation(t *testing.T) {
	text := "Phở là món ăn nổi tiếng của Hà Nội. " +
		"TS. Nguyễn Văn A nghiên cứu kinh tế vĩ mô. " +
		"Ô tô điện đang dần phổ biến tại các thành phố lớn. " +
		"Xe máy vẫn là phương tiện chính của người dân."
	c, err := NewSemantic(fakeTopicEmbedder{}, SemanticOption{MinChunk: 10, Percentile: 0.9})
	if err != nil {
		t.Fatalf("NewSemantic: %v", err)
	}
	got, err := c.Split(context.Background(), text)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 chunk theo cụm nghĩa, got %d: %q", len(got), got)
	}
	if !strings.Contains(got[0], "Nguyễn Văn A") {
		t.Fatalf("chunk 0 mất nội dung viết tắt: %q", got[0])
	}
	if !strings.Contains(got[1], "Xe máy") {
		t.Fatalf("chunk 1 mất cụm xe cộ: %q", got[1])
	}
}

func TestNewSemanticNilEmbedder(t *testing.T) {
	if _, err := NewSemantic(nil, SemanticOption{}); err == nil {
		t.Fatal("expected lỗi với embedder nil")
	}
}
