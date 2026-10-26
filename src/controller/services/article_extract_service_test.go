package services

import (
	"context"
	"testing"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/extractor"
	"knowledge_ingestion/src/domain"
)

// extractStubPages là vai IArticlePageRepository cho extractRun: nhớ lại TỪNG
// lô được ghi để test biết flushTail có ghi thừa trang cũ hay không.
type extractStubPages struct {
	saved   []*domain.ArticlePage
	batches [][]*domain.ArticlePage
}

func (p *extractStubPages) ListByArticle(context.Context, int64) ([]*domain.ArticlePage, error) {
	return p.saved, nil
}

func (p *extractStubPages) CreateBatch(_ context.Context, pages []*domain.ArticlePage) error {
	p.batches = append(p.batches, append([]*domain.ArticlePage(nil), pages...))
	return nil
}

func (p *extractStubPages) ListPagesRange(context.Context, int64, int, int) ([]*domain.ArticlePage, error) {
	return nil, nil
}
func (p *extractStubPages) Progress(context.Context, int64) (*domain.PageProgress, error) {
	return &domain.PageProgress{DoneSeq: -1}, nil
}
func (p *extractStubPages) DeleteByArticleID(context.Context, int64) error  { return nil }
func (p *extractStubPages) DeleteByArticles(context.Context, []int64) error { return nil }
func (p *extractStubPages) DeleteOrphan(context.Context) (int64, error)     { return 0, nil }

var _ domain.IArticlePageRepository = (*extractStubPages)(nil)

// TestExtractRunWritesOnlyNewPages khoá đúng thứ từng là closure ẩn trong
// extractOne: visit bỏ qua seq đã có ở lần chạy trước, và flushTail chỉ ghi
// ĐUÔI MỚI. Ghi lại cả danh sách thì mỗi lô resume lại insert lại toàn bộ phần
// đã lưu (ON CONFLICT giữ nguyên nhưng vẫn tốn I/O).
func TestExtractRunWritesOnlyNewPages(t *testing.T) {
	pages := &extractStubPages{saved: []*domain.ArticlePage{
		{ArticleID: 1, Seq: 1, PageNum: 1, Text: "trang cũ"},
	}}
	svc := &articleExtractService{pageRepo: pages}
	r := newExtractRun(svc, context.Background(), "trace", &domain.Article{BaseModel: domain.BaseModel{ID: 1}})

	if err := r.loadSaved(); err != nil {
		t.Fatalf("loadSaved: %v", err)
	}
	// Trang đã lưu: bỏ qua, không vào lô, không tăng sinceFlush.
	if err := r.visit(extractor.Page{Seq: 1, Num: 1, Text: "trang cũ"}); err != nil {
		t.Fatalf("visit trang cũ: %v", err)
	}
	if r.sinceFlush != 0 || len(r.fresh) != 0 {
		t.Fatalf("visit trang đã lưu lại gom vào lô: fresh=%d sinceFlush=%d", len(r.fresh), r.sinceFlush)
	}

	// Đủ 1 lô trang mới → ghi đúng 1 batch, chỉ chứa trang mới; thừa 1 trang
	// thì nó nằm lại chờ flushTail.
	for seq := 2; seq <= constants.EXTRACT_FLUSH_PAGES+2; seq++ {
		if err := r.visit(extractor.Page{Seq: seq, Num: seq, Text: "trang mới"}); err != nil {
			t.Fatalf("visit seq %d: %v", seq, err)
		}
	}
	if len(pages.batches) != 1 {
		t.Fatalf("ghi %d lô, muốn 1", len(pages.batches))
	}
	if got, want := len(pages.batches[0]), constants.EXTRACT_FLUSH_PAGES; got != want {
		t.Errorf("lô đầu có %d trang, muốn %d", got, want)
	}
	for _, p := range pages.batches[0] {
		if p.Seq == 1 {
			t.Error("ghi lại trang đã lưu ở lần chạy trước")
		}
	}
	// Còn 1 trang dư chưa ghi: flushTail ghi nốt đúng 1 trang đó.
	if err := r.flushTail(); err != nil {
		t.Fatalf("flushTail: %v", err)
	}
	if len(pages.batches) != 2 {
		t.Fatalf("sau flushTail có %d lô, muốn 2", len(pages.batches))
	}
	if got := len(pages.batches[1]); got != 1 {
		t.Errorf("lô cuối có %d trang, muốn 1 (chỉ phần dư)", got)
	}
	if r.sinceFlush != 0 {
		t.Errorf("flushTail xong mà sinceFlush = %d, muốn 0", r.sinceFlush)
	}
	// Gọi lại không được ghi lô rỗng.
	if err := r.flushTail(); err != nil {
		t.Fatalf("flushTail lần 2: %v", err)
	}
	if len(pages.batches) != 2 {
		t.Errorf("flushTail rỗng vẫn ghi lô: %d lô", len(pages.batches))
	}
}
