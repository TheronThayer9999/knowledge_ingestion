package eino

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"knowledge_ingestion/src/domain"
)

// fakeTextPages là vai hẹp ArticlePageText: trả mảnh theo seq giống repo thật
// (lọc seq >= fromSeq, tối đa limit) — đủ để test cửa sổ và điều kiện dừng.
type fakeTextPages struct {
	pages   []*domain.ArticlePage
	err     error
	calls   int
	lastArt int64
}

func (f *fakeTextPages) ListPagesRange(_ context.Context, articleID int64, fromSeq, limit int) ([]*domain.ArticlePage, error) {
	f.calls++
	f.lastArt = articleID
	if f.err != nil {
		return nil, f.err
	}
	out := make([]*domain.ArticlePage, 0, limit)
	for _, p := range f.pages {
		if p.Seq >= fromSeq && len(out) < limit {
			out = append(out, p)
		}
	}
	return out, nil
}

// sheetPages dựng article_pages của 1 file Excel 2 sheet:
// Bảng lương có header + 3 hàng, Phụ lục có header + 1 hàng.
func sheetPages() []*domain.ArticlePage {
	return []*domain.ArticlePage{{
		Seq:  1,
		Text: "## Sheet: Bảng lương\n| Họ tên | Lương |\n| An | 10 |\n| Bình | 20 |\n| Chi | 30 |\n\n## Sheet: Phụ lục\n| Ghi chú |\n| Không |\n",
	}}
}

func sheetTestArticle() *domain.Article {
	return &domain.Article{BaseModel: domain.BaseModel{ID: 10}, CategoryID: 3, StorageKey: "uploads/luong.xlsx", Name: "luong.xlsx"}
}

func sheetScopeCtx() context.Context {
	return WithScope(context.Background(), Scope{UserID: 7, CategoryIDs: []int64{3}})
}

func runListSheets(t *testing.T, articles domain.IArticleRepository, pages domain.ArticlePageText, args string) *listSheetsResult {
	t.Helper()
	tool, err := NewListSheetsTool(articles, pages)
	if err != nil {
		t.Fatalf("NewListSheetsTool: %v", err)
	}
	raw, err := tool.InvokableRun(sheetScopeCtx(), args)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	var got listSheetsResult
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("output phải JSON parse được, got %q: %v", raw, err)
	}
	return &got
}

func runReadSheet(t *testing.T, articles domain.IArticleRepository, pages domain.ArticlePageText, args string) *readSheetResult {
	t.Helper()
	tool, err := NewReadSheetTool(articles, pages)
	if err != nil {
		t.Fatalf("NewReadSheetTool: %v", err)
	}
	raw, err := tool.InvokableRun(sheetScopeCtx(), args)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	var got readSheetResult
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("output phải JSON parse được, got %q: %v", raw, err)
	}
	return &got
}

func TestNewSheetToolsRequireDeps(t *testing.T) {
	if _, err := NewListSheetsTool(nil, &fakeTextPages{}); err == nil {
		t.Fatal("list_sheets: thiếu article repo phải fail-fast lúc startup")
	}
	if _, err := NewListSheetsTool(&fakeIssueArticles{}, nil); err == nil {
		t.Fatal("list_sheets: thiếu page repo phải fail-fast lúc startup")
	}
	if _, err := NewReadSheetTool(nil, &fakeTextPages{}); err == nil {
		t.Fatal("read_sheet: thiếu article repo phải fail-fast lúc startup")
	}
	if _, err := NewReadSheetTool(&fakeIssueArticles{}, nil); err == nil {
		t.Fatal("read_sheet: thiếu page repo phải fail-fast lúc startup")
	}
}

func TestSheetToolsRequireScope(t *testing.T) {
	listTool, err := NewListSheetsTool(&fakeIssueArticles{}, &fakeTextPages{})
	if err != nil {
		t.Fatalf("NewListSheetsTool: %v", err)
	}
	if _, err := listTool.InvokableRun(context.Background(), `{"article_id":10}`); err == nil ||
		!strings.Contains(err.Error(), "scope") {
		t.Fatalf("list_sheets thiếu scope phải lỗi nhắc scope, got %v", err)
	}
	readTool, err := NewReadSheetTool(&fakeIssueArticles{}, &fakeTextPages{})
	if err != nil {
		t.Fatalf("NewReadSheetTool: %v", err)
	}
	if _, err := readTool.InvokableRun(context.Background(), `{"article_id":10,"sheet":"1"}`); err == nil ||
		!strings.Contains(err.Error(), "scope") {
		t.Fatalf("read_sheet thiếu scope phải lỗi nhắc scope, got %v", err)
	}
}

// TestListSheetsTool là câu hỏi gốc của tính năng: "file Excel có sheet nào".
func TestListSheetsTool(t *testing.T) {
	pages := &fakeTextPages{pages: sheetPages()}
	got := runListSheets(t, &fakeIssueArticles{article: sheetTestArticle()}, pages, `{"article_id":10}`)
	if got.SheetCount != 2 || len(got.Sheets) != 2 {
		t.Fatalf("want 2 sheet, got %+v", got)
	}
	first := got.Sheets[0]
	if first.Index != 1 || first.Name != "Bảng lương" || first.Rows != 3 {
		t.Fatalf("sheet 1 sai: %+v", first)
	}
	if len(first.Columns) != 2 || first.Columns[0] != "Họ tên" || first.Columns[1] != "Lương" {
		t.Fatalf("columns sai: %+v", first.Columns)
	}
	if got.Sheets[1].Name != "Phụ lục" || got.Sheets[1].Rows != 1 {
		t.Fatalf("sheet 2 sai: %+v", got.Sheets[1])
	}
	if pages.lastArt != 10 {
		t.Fatalf("phải đọc đúng article_id, got %d", pages.lastArt)
	}
	if got.Note != "" {
		t.Fatalf("file sạch không cần note, got %q", got.Note)
	}
}

// Gọi nhầm tool lên PDF/docx: 1 câu SELECT đầu là đủ để nói thật, không quét
// hết cả tài liệu.
func TestListSheetsToolNotExcel(t *testing.T) {
	pages := &fakeTextPages{pages: []*domain.ArticlePage{{Seq: 1, Text: "Điều 1. Phạm vi áp dụng"}}}
	got := runListSheets(t, &fakeIssueArticles{article: sheetTestArticle()}, pages, `{"article_id":10}`)
	if got.SheetCount != 0 || len(got.Sheets) != 0 {
		t.Fatalf("văn bản thường không có sheet: %+v", got)
	}
	if !strings.Contains(got.Note, "không có sheet") {
		t.Fatalf("note phải nói rõ không có sheet, got %q", got.Note)
	}
}

func TestListSheetsToolNotExtracted(t *testing.T) {
	got := runListSheets(t, &fakeIssueArticles{article: sheetTestArticle()}, &fakeTextPages{}, `{"article_id":10}`)
	if !strings.Contains(got.Note, "chưa trích xong") {
		t.Fatalf("chưa có trang phải báo chưa trích, got %q", got.Note)
	}
}

// TestReadSheetToolPaging: đọc theo hàng, next_from_row nối tiếp — LLM không
// bao giờ nhận cả sheet 20k hàng trong 1 lần gọi.
func TestReadSheetToolPaging(t *testing.T) {
	articles := &fakeIssueArticles{article: sheetTestArticle()}
	pages := &fakeTextPages{pages: sheetPages()}

	first := runReadSheet(t, articles, pages, `{"article_id":10,"sheet":"Bảng lương","from_row":2,"max_rows":1}`)
	if first.Index != 1 || first.Sheet != "Bảng lương" || first.TotalRows != 3 {
		t.Fatalf("metadata sai: %+v", first)
	}
	if first.FromRow != 2 || first.ToRow != 2 || first.NextFromRow != 3 {
		t.Fatalf("phân trang sai: %+v", first)
	}
	if first.Text != "| Họ tên | Lương |\n| Bình | 20 |" {
		t.Fatalf("text phải gồm header + đúng 1 hàng, got %q", first.Text)
	}
	if !strings.Contains(first.Note, "from_row=3") {
		t.Fatalf("note phải chỉ cách đọc tiếp, got %q", first.Note)
	}

	last := runReadSheet(t, articles, pages, `{"article_id":10,"sheet":"1","from_row":3,"max_rows":10}`)
	if last.NextFromRow != 0 {
		t.Fatalf("hết hàng thì next_from_row = 0, got %d", last.NextFromRow)
	}
	if last.ToRow != 3 || !strings.Contains(last.Text, "| Chi | 30 |") {
		t.Fatalf("trang cuối sai: %+v", last)
	}
}

// Chọn sheet bằng số thứ tự và bằng tên đều phải ra cùng sheet; tên khớp
// chính xác phải thắng khớp chứa ("Phí" vs "Phụ phí").
func TestReadSheetToolSelectsByIdxAndName(t *testing.T) {
	pages := &fakeTextPages{pages: []*domain.ArticlePage{{
		Seq:  1,
		Text: "## Sheet: Phí\n| a |\n| 1 |\n\n## Sheet: Phụ phí\n| b |\n| 2 |\n",
	}}}
	articles := &fakeIssueArticles{article: sheetTestArticle()}

	byIdx := runReadSheet(t, articles, pages, `{"article_id":10,"sheet":"2"}`)
	if byIdx.Sheet != "Phụ phí" || !strings.Contains(byIdx.Text, "| 2 |") {
		t.Fatalf("chọn theo số sai: %+v", byIdx)
	}
	exact := runReadSheet(t, articles, pages, `{"article_id":10,"sheet":"Phí"}`)
	if exact.Sheet != "Phí" || !strings.Contains(exact.Text, "| 1 |") {
		t.Fatalf("khớp chính xác phải thắng khớp chứa: %+v", exact)
	}
	ambiguous := runReadSheet(t, articles, pages, `{"article_id":10,"sheet":"phí phụ"}`)
	if !strings.Contains(ambiguous.Note, "không có sheet") {
		t.Fatalf("không khớp phải báo không có sheet, got %q", ambiguous.Note)
	}
}

// Nhiều sheet mà LLM không chỉ định: trả note liệt kê, không lặng lẽ đọc sheet 1.
func TestReadSheetToolNeedsSheetWhenMany(t *testing.T) {
	got := runReadSheet(t, &fakeIssueArticles{article: sheetTestArticle()},
		&fakeTextPages{pages: sheetPages()}, `{"article_id":10}`)
	if got.Note == "" || !strings.Contains(got.Note, "Bảng lương") {
		t.Fatalf("phải yêu cầu chọn sheet và liệt kê tên, got %+v", got)
	}
	if got.Text != "" || got.TotalRows != 0 {
		t.Fatalf("chưa chọn sheet thì không được trả dữ liệu: %+v", got)
	}
}

// Một sheet duy nhất thì bỏ trống sheet vẫn đọc được — LLM hay quên args này.
func TestReadSheetToolSingleSheet(t *testing.T) {
	pages := &fakeTextPages{pages: []*domain.ArticlePage{{Seq: 1, Text: "## Sheet: Duy nhất\n| c |\n| 9 |\n"}}}
	got := runReadSheet(t, &fakeIssueArticles{article: sheetTestArticle()}, pages, `{"article_id":10}`)
	if got.Sheet != "Duy nhất" || !strings.Contains(got.Text, "| 9 |") {
		t.Fatalf("1 sheet phải tự chọn: %+v", got)
	}
}

// from_row vượt quá là args sai: trả header + số hàng thật để LLM tự sửa,
// không error làm sập run.
func TestReadSheetToolFromRowBeyondEnd(t *testing.T) {
	got := runReadSheet(t, &fakeIssueArticles{article: sheetTestArticle()},
		&fakeTextPages{pages: sheetPages()}, `{"article_id":10,"sheet":"1","from_row":99}`)
	if !strings.Contains(got.Note, "chỉ có 3 hàng") {
		t.Fatalf("note phải nói số hàng thật, got %q", got.Note)
	}
	if got.Text != "| Họ tên | Lương |" {
		t.Fatalf("phải trả header để LLM thấy cột, got %q", got.Text)
	}
}

// Scope: bài ngoài category của request phải trả cùng câu với "không tìm
// thấy", không lộ là bài tồn tại; bài không tồn tại cũng vậy.
func TestSheetToolsRespectScope(t *testing.T) {
	outOfScope := sheetTestArticle()
	outOfScope.CategoryID = 99
	got := runListSheets(t, &fakeIssueArticles{article: outOfScope}, &fakeTextPages{pages: sheetPages()}, `{"article_id":10}`)
	if !strings.Contains(got.Note, "không tìm thấy") || got.SheetCount != 0 {
		t.Fatalf("ngoài scope không được trả sheet: %+v", got)
	}
	missing := runReadSheet(t, &fakeIssueArticles{article: nil}, &fakeTextPages{pages: sheetPages()}, `{"article_id":10,"sheet":"1"}`)
	if !strings.Contains(missing.Note, "không tìm thấy") || missing.Text != "" {
		t.Fatalf("bài không tồn tại không được trả dữ liệu: %+v", missing)
	}
	badID := runListSheets(t, &fakeIssueArticles{article: sheetTestArticle()}, &fakeTextPages{pages: sheetPages()}, `{"article_id":"None"}`)
	if !strings.Contains(badID.Note, "article_id") {
		t.Fatalf("article_id rác phải trả note sửa args, got %q", badID.Note)
	}
}
