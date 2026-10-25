package eino

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"knowledge_ingestion/src/domain"
)

// fakeIssueArticles chỉ override GetByID — các method khác của
// IArticleRepository nhúng nil (gọi nhầm sẽ panic, cố ý: test này chỉ đi
// đường scope).
type fakeIssueArticles struct {
	domain.IArticleRepository
	article *domain.Article
	err     error
	gotUser int64
}

func (f *fakeIssueArticles) GetByID(_ context.Context, _ int64, userID int64) (*domain.Article, error) {
	f.gotUser = userID
	return f.article, f.err
}

// fakeIssuePages là vai hẹp chỉ có 1 method — đúng lý do tách
// domain.ArticlePageIssues khỏi IArticlePageRepository.
type fakeIssuePages struct {
	issues []domain.PageIssue
	err    error
	called bool
	gotID  int64
	gotMin int
}

func (f *fakeIssuePages) ListPageIssues(_ context.Context, articleID int64, minChars int) ([]domain.PageIssue, error) {
	f.called = true
	f.gotID, f.gotMin = articleID, minChars
	return f.issues, f.err
}

func newIssuesTool(t *testing.T, articles domain.IArticleRepository, pages domain.ArticlePageIssues) *unreadablePagesResult {
	t.Helper()
	tool, err := NewListUnreadablePagesTool(articles, pages)
	if err != nil {
		t.Fatalf("NewListUnreadablePagesTool: %v", err)
	}
	ctx := WithScope(context.Background(), Scope{UserID: 7, CategoryIDs: []int64{3}})
	raw, err := tool.InvokableRun(ctx, `{"article_id":10}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	var got unreadablePagesResult
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("output phải JSON parse được, got %q: %v", raw, err)
	}
	return &got
}

func TestNewListUnreadablePagesToolRequiresDeps(t *testing.T) {
	if _, err := NewListUnreadablePagesTool(nil, &fakeIssuePages{}); err == nil {
		t.Fatal("article repo nil phải lỗi để fail-fast lúc startup")
	}
	if _, err := NewListUnreadablePagesTool(&fakeIssueArticles{}, nil); err == nil {
		t.Fatal("page repo nil phải lỗi để fail-fast lúc startup")
	}
}

func TestListUnreadablePagesRequiresScope(t *testing.T) {
	tool, err := NewListUnreadablePagesTool(&fakeIssueArticles{}, &fakeIssuePages{})
	if err != nil {
		t.Fatalf("NewListUnreadablePagesTool: %v", err)
	}
	if _, err := tool.InvokableRun(context.Background(), `{"article_id":10}`); err == nil ||
		!strings.Contains(err.Error(), "scope") {
		t.Fatalf("thiếu scope phải lỗi nhắc scope, got %v", err)
	}
}

// Args vô nghĩa + bài không tồn tại/ngoài scope đều KHÔNG được lỗi (LLM sẽ
// tự xoay) và tuyệt đối không được chạm page repo khi scope đã chặn.
func TestListUnreadablePagesGuardsBeforeQuery(t *testing.T) {
	cases := []struct {
		name     string
		articles *fakeIssueArticles
		note     string
	}{
		{
			name:     "không tìm thấy",
			articles: &fakeIssueArticles{err: domain.ErrNotFound},
			note:     "không tìm thấy tài liệu",
		},
		{
			name:     "ngoài category scope",
			articles: &fakeIssueArticles{article: &domain.Article{BaseModel: domain.BaseModel{ID: 10}, CategoryID: 99}},
			note:     "không tìm thấy tài liệu",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pages := &fakeIssuePages{issues: []domain.PageIssue{{PageNum: 1}}}
			got := newIssuesTool(t, tc.articles, pages)
			if pages.called {
				t.Fatal("scope đã chặn thì không được query article_pages")
			}
			if got.IssueCount != 0 || len(got.Issues) != 0 {
				t.Fatalf("kết quả phải rỗng, got %+v", got)
			}
			if !strings.Contains(got.Note, tc.note) {
				t.Fatalf("note phải chứa %q, got %q", tc.note, got.Note)
			}
		})
	}

	pages := &fakeIssuePages{}
	tool, err := NewListUnreadablePagesTool(&fakeIssueArticles{}, pages)
	if err != nil {
		t.Fatalf("NewListUnreadablePagesTool: %v", err)
	}
	ctx := WithScope(context.Background(), Scope{UserID: 7})
	raw, err := tool.InvokableRun(ctx, `{"article_id":"None"}`)
	if err != nil {
		t.Fatalf("article_id không phải số phải trả note, không error: %v", err)
	}
	var got unreadablePagesResult
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("output phải JSON: %q: %v", raw, err)
	}
	if got.Note == "" || pages.called {
		t.Fatalf("article_id vô nghĩa phải có note và không query DB, got %+v", got)
	}
}

// Đường chính: đếm đúng + note nói rõ thiếu trang, userID lấy từ scope.
func TestListUnreadablePagesReportsIssues(t *testing.T) {
	articles := &fakeIssueArticles{article: &domain.Article{BaseModel: domain.BaseModel{ID: 10}, CategoryID: 3, PageCount: 12}}
	pages := &fakeIssuePages{issues: []domain.PageIssue{
		{PageNum: 3, Seq: 3, Chars: 0},
		{PageNum: 7, Seq: 7, Chars: 12, LayoutScore: 0.6},
	}}
	got := newIssuesTool(t, articles, pages)
	if articles.gotUser != 7 {
		t.Fatalf("GetByID phải khóa user trong scope (7), got %d", articles.gotUser)
	}
	if pages.gotID != 10 || pages.gotMin != unreadableDefaultMinChars {
		t.Fatalf("query phải đúng bài + ngưỡng mặc định, got id=%d min=%d", pages.gotID, pages.gotMin)
	}
	if got.PageCount != 12 || got.IssueCount != 2 || len(got.Issues) != 2 {
		t.Fatalf("đếm sai: %+v", got)
	}
	if got.Issues[0].PageNum != 3 || got.Issues[1].Chars != 12 || got.Issues[1].LayoutScore != 0.6 {
		t.Fatalf("chi tiết trang sai: %+v", got.Issues)
	}
	if !strings.Contains(got.Note, "2/12 trang") || !strings.Contains(got.Note, "KHÔNG suy đoán") {
		t.Fatalf("note phải nêu tỉ lệ + cấm suy đoán, got %q", got.Note)
	}
}

// Bài chưa trích xong (page_count = 0) không được coi là "sạch".
func TestListUnreadablePagesNotExtractedYet(t *testing.T) {
	articles := &fakeIssueArticles{article: &domain.Article{BaseModel: domain.BaseModel{ID: 10}, CategoryID: 3}}
	got := newIssuesTool(t, articles, &fakeIssuePages{})
	if got.IssueCount != 0 || got.Note == "" || !strings.Contains(got.Note, "chưa trích xong") {
		t.Fatalf("bài chưa trích phải có note cảnh báo, got %+v", got)
	}
}

// Cả file scan hỏng (60 trang) không được đẩy 60 dòng vào context: tổng vẫn
// đúng, danh sách cắt ở trần.
func TestListUnreadablePagesTruncates(t *testing.T) {
	issues := make([]domain.PageIssue, 60)
	for i := range issues {
		issues[i] = domain.PageIssue{PageNum: i + 1, Seq: i + 1}
	}
	articles := &fakeIssueArticles{article: &domain.Article{BaseModel: domain.BaseModel{ID: 10}, CategoryID: 3, PageCount: 60}}
	got := newIssuesTool(t, articles, &fakeIssuePages{issues: issues})
	if got.IssueCount != 60 || len(got.Issues) != unreadableMaxIssues {
		t.Fatalf("phải cắt ở %d nhưng vẫn báo tổng 60, got count=%d len=%d", unreadableMaxIssues, got.IssueCount, len(got.Issues))
	}
	if !strings.Contains(got.Note, "chỉ liệt kê 50 trang đầu") {
		t.Fatalf("note phải nói rõ đã cắt, got %q", got.Note)
	}
}

func TestListUnreadablePagesClampsMinChars(t *testing.T) {
	articles := &fakeIssueArticles{article: &domain.Article{BaseModel: domain.BaseModel{ID: 10}, CategoryID: 3, PageCount: 5}}
	for _, tc := range []struct{ in, want int }{
		{0, unreadableDefaultMinChars},
		{-5, unreadableDefaultMinChars},
		{10, 10},
		{5000, unreadableMaxMinChars},
	} {
		pages := &fakeIssuePages{}
		tool, err := NewListUnreadablePagesTool(articles, pages)
		if err != nil {
			t.Fatalf("NewListUnreadablePagesTool: %v", err)
		}
		ctx := WithScope(context.Background(), Scope{UserID: 7, CategoryIDs: []int64{3}})
		if _, err := tool.InvokableRun(ctx, `{"article_id":10,"min_chars":`+strconv.Itoa(tc.in)+`}`); err != nil {
			t.Fatalf("InvokableRun(min_chars=%d): %v", tc.in, err)
		}
		if pages.gotMin != tc.want {
			t.Fatalf("min_chars %d phải thành %d, got %d", tc.in, tc.want, pages.gotMin)
		}
	}
}
