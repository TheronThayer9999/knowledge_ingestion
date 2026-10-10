package chunker

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/schema"
	einorecursive "github.com/cloudwego/eino-ext/components/document/transformer/splitter/recursive"
	einosemantic "github.com/cloudwego/eino-ext/components/document/transformer/splitter/semantic"

	"knowledge_ingestion/src/domain"
)

// Chunker chiến lược cắt text thành chunk — service chunk chỉ phụ thuộc
// interface này nên đổi recursive/semantic/manual không phải sửa service.
// Trả error vì tầng semantic gọi embed (lỗi mạng Ollama) — service map thành
// transient để backoff, không failed oan như lỗi file hỏng.
type Chunker interface {
	Split(ctx context.Context, text string) ([]string, error)
}

// Manual bọc Split thủ công cũ (đo rune, đã có test) — giữ cho test service +
// fallback khi cần hành vi cũ byte-identical. Production mặc định dùng
// Recursive (Eino) qua config worker.chunker.
type Manual struct {
	Opt Option
}

func (m Manual) Split(_ context.Context, text string) ([]string, error) {
	return Split(text, m.Opt), nil
}

// vietnameseSeparators thứ tự dấu cắt cho tiếng Việt, từ biên lớn tới biên
// nhỏ — khác default Eino (["\n", ".", "?", "!"]) ở chỗ thêm "…", "..." (văn
// Việt dùng nhiều) và ";", ":" (điều khoản, liệt kê hành chính). " " và ""
// cuối là lưới an toàn: đoạn dài không dấu câu thì cắt theo từ, bí nữa thì
// cắt theo ký tự — Eino không có là trả nguyên đoạn quá cỡ.
func vietnameseSeparators() []string {
	return []string{"\n\n", "\n", "…", "...", ".", "?", "!", ";", ":", " ", ""}
}

// runeLen đo độ dài theo rune — chữ Việt có dấu chiếm 2–3 byte UTF-8 nên
// dùng len() của Eino mặc định sẽ cho chunk nhỏ hơn 2–3 lần ý định.
func runeLen(s string) int { return utf8.RuneCountInString(s) }

// sentenceSeparators dấu cắt cấp câu cho semantic splitter — splitter này
// tự tách thô thành câu rồi mới embed tính tương đồng, nên chỉ cho dấu cấp
// câu (không cho " "/""/";"/":" như recursive, kẻo tách vụn tới từ/ký tự
// ngay từ đầu rồi ngữ nghĩa không còn ý nghĩa).
func sentenceSeparators() []string {
	return []string{"\n\n", "\n", "…", "...", ".", "?", "!"}
}

// einoChunker bọc document.Transformer của Eino thành Chunker của repo —
// splitter build 1 lần ở loader rồi tái dùng (stateless, an toàn concurrent
// như worker pool chunk gọi song song).
type einoChunker struct {
	t document.Transformer
}

func (c *einoChunker) Split(ctx context.Context, text string) ([]string, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	docs, err := c.t.Transform(ctx, []*schema.Document{{Content: text}})
	if err != nil {
		return nil, fmt.Errorf("eino split: %w", err)
	}
	out := make([]string, 0, len(docs))
	for _, d := range docs {
		if s := strings.TrimSpace(d.Content); s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

// NewRecursive dựng tầng 1 (bulk ingestion): splitter đệ quy của Eino, cùng
// họ thuật toán với Split thủ công (thử dấu sạch trước, quá dài thì đệ quy
// dấu nhỏ hơn, gộp + overlap) nhưng là implementation chuẩn có KeepType/
// IDGenerator. Tái dùng Option hiện tại — zero value là default tiếng Việt.
func NewRecursive(opt Option) (Chunker, error) {
	size := opt.ChunkSize
	if size <= 0 {
		size = DefaultChunkSize
	}
	overlap := opt.Overlap
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= size {
		overlap = size - 1
	}
	seps := opt.Separators
	if len(seps) == 0 {
		seps = vietnameseSeparators()
	}
	t, err := einorecursive.NewSplitter(context.Background(), &einorecursive.Config{
		ChunkSize:   size,
		OverlapSize: overlap,
		Separators:  seps,
		LenFunc:     runeLen,
	})
	if err != nil {
		return nil, fmt.Errorf("tạo recursive splitter: %w", err)
	}
	return &einoChunker{t: t}, nil
}

// SemanticOption tinh chỉnh tầng 2 (tài liệu ưu tiên).
type SemanticOption struct {
	// MinChunk là số rune tối thiểu — chunk nhỏ hơn thì gộp với chunk cạnh.
	// <=0 thì dùng DefaultSemanticMinChunk.
	MinChunk int
	// Buffer là số câu ngữ cảnh lấy thêm 2 bên mỗi câu khi embed để tính
	// tương đồng. 0 là embed từng câu độc lập (rẻ nhất).
	Buffer int
	// Percentile là ngưỡng cắt (0,1] — cặp câu có tương đồng rớt dưới phân vị
	// này thì cắt. <=0 thì dùng 0.9 (mặc định Eino).
	Percentile float64
}

// DefaultSemanticMinChunk chặn chunk vụn 1 câu ngắn (vd "Điều 1.") đứng riêng
// — gộp vào điều liền kề để retrieval đủ ngữ cảnh.
const DefaultSemanticMinChunk = 100

// embedderAdapter bọc domain.Embedder (client Ollama sẵn có của repo) thành
// embedding.Embedder của Eino cho semantic splitter — khỏi thêm client thứ
// hai, giữ nguyên config/timeout/retry hiện tại. Chỉ chuyển float32→float64.
type embedderAdapter struct {
	emb domain.Embedder
}

func (a *embedderAdapter) EmbedStrings(ctx context.Context, texts []string, _ ...embedding.Option) ([][]float64, error) {
	vecs, err := a.emb.EmbedBatch(ctx, texts)
	if err != nil {
		return nil, err
	}
	out := make([][]float64, len(vecs))
	for i, v := range vecs {
		f := make([]float64, len(v))
		for j, x := range v {
			f[j] = float64(x)
		}
		out[i] = f
	}
	return out, nil
}

// NewSemantic dựng tầng 2 (tài liệu quan trọng / văn bản hành chính): cắt
// theo độ tương đồng ngữ nghĩa nên miễn nhiễm viết tắt ("TS.", "P.", "Q.")
// mà splitter ký tự nào cũng chẻ nhầm. Đắt hơn recursive (mỗi câu tốn call
// embed) nên chỉ dùng cho tài liệu ưu tiên, xem config worker.chunker.
func NewSemantic(emb domain.Embedder, opt SemanticOption) (Chunker, error) {
	if emb == nil {
		return nil, fmt.Errorf("semantic cần embedder, got nil")
	}
	min := opt.MinChunk
	if min <= 0 {
		min = DefaultSemanticMinChunk
	}
	pct := opt.Percentile
	if pct <= 0 {
		pct = 0.9
	}
	buf := opt.Buffer
	if buf < 0 {
		buf = 0
	}
	t, err := einosemantic.NewSplitter(context.Background(), &einosemantic.Config{
		Embedding:    &embedderAdapter{emb: emb},
		BufferSize:   buf,
		MinChunkSize: min,
		Separators:   sentenceSeparators(),
		LenFunc:      runeLen,
		Percentile:   pct,
	})
	if err != nil {
		return nil, fmt.Errorf("tạo semantic splitter: %w", err)
	}
	return &einoChunker{t: t}, nil
}
