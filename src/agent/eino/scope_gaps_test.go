package eino

import (
	"context"
	"encoding/json"
	"testing"

	"knowledge_ingestion/src/domain"
)

// Bộ test regression cho 4 lỗ hổng scope đã fix (xem
// technical_debt/SCOPE_ENFORCEMENT_GAPS.md). Chạy mặc định — không còn gate
// RUN_SCOPE_GAP_TESTS; tháo fix là các test này đỏ để chặn tái phát.

// fakeScopedArticleRepo chỉ override 2 method mà article_info gọi; các method
// còn lại của IArticleRepository để nil (gọi nhầm sẽ panic — cố ý, test này
// chỉ quan tâm đường scope).
type fakeScopedArticleRepo struct {
	domain.IArticleRepository
	article        *domain.Article
	articlesByCat  []*domain.Article
	listedCategory []int64
}

func (f *fakeScopedArticleRepo) GetByID(_ context.Context, _ int64, _ int64) (*domain.Article, error) {
	return f.article, nil
}

func (f *fakeScopedArticleRepo) ListByCategoryID(_ context.Context, categoryID, _ int64, _, _ int) ([]*domain.Article, error) {
	f.listedCategory = append(f.listedCategory, categoryID)
	return f.articlesByCat, nil
}

// fakeScopedCategoryRepo chỉ cần GetAll — đúng method list_categories gọi.
type fakeScopedCategoryRepo struct {
	domain.ICategoryRepositoryImpl
	all []*domain.Category
}

func (f *fakeScopedCategoryRepo) GetAll(_ context.Context, _ int64) ([]*domain.Category, error) {
	return f.all, nil
}

// GAP-1 (regression): request khoanh scope [9], LLM truyền category_id=8 —
// DTO phải thắng LLM (như knowledge_search), tool không được đọc chủ đề
// ngoài scope.
func TestScopeGap1ArticleInfoLLMCategoryCannotEscapeScope(t *testing.T) {
	repo := &fakeScopedArticleRepo{
		articlesByCat: []*domain.Article{
			{BaseModel: domain.BaseModel{ID: 20}, CategoryID: 8, Name: "bài của chủ đề 8"},
		},
	}
	tool, err := NewArticleInfoTool(repo)
	if err != nil {
		t.Fatalf("NewArticleInfoTool: %v", err)
	}
	ctx := WithScope(context.Background(), Scope{UserID: 3, CategoryIDs: []int64{9}})
	raw, err := tool.InvokableRun(ctx, `{"category_id":8}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	var got []articleInfoItem
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("output phải JSON: %q: %v", raw, err)
	}
	if len(got) != 0 {
		t.Fatalf("GAP-1: LLM truyền category_id=8 ngoài scope [9] nhưng vẫn nhận %d bài: %+v", len(got), got)
	}
	for _, cid := range repo.listedCategory {
		if cid != 9 {
			t.Fatalf("GAP-1: tool đã truy vấn chủ đề ngoài scope %d", cid)
		}
	}
}

// GAP-2 (regression): scope [9] nhưng article_id trỏ bài thuộc chủ đề 8 —
// article_info chế độ 1 bài phải kiểm cả category trước khi trả metadata.
func TestScopeGap2ArticleInfoSingleArticleOutsideScope(t *testing.T) {
	repo := &fakeScopedArticleRepo{
		article: &domain.Article{BaseModel: domain.BaseModel{ID: 20}, CategoryID: 8, Name: "bài ngoài scope"},
	}
	tool, err := NewArticleInfoTool(repo)
	if err != nil {
		t.Fatalf("NewArticleInfoTool: %v", err)
	}
	ctx := WithScope(context.Background(), Scope{UserID: 3, CategoryIDs: []int64{9}})
	raw, err := tool.InvokableRun(ctx, `{"article_id":20}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	var got []articleInfoItem
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("output phải JSON: %q: %v", raw, err)
	}
	if len(got) != 0 {
		t.Fatalf("GAP-2: metadata bài thuộc chủ đề 8 lọt qua scope [9]: %+v", got)
	}
}

// GAP-3 (regression): đọc bài ngoài scope — ScrollRange rỗng (đúng, Qdrant
// chặn) và total_chunk phải đếm bằng CountByFilter cùng scope, không dùng
// CountByArticle toàn cục.
func TestScopeGap3ReadArticleDoesNotLeakTotalChunk(t *testing.T) {
	// total=6 mô phỏng CountByArticle toàn cục (nếu dùng nhầm); scopedTotal=0
	// là kết quả đúng của CountByFilter với scope [9] cho bài chủ đề 8.
	v := &fakeArticleVectors{total: 6, scopedTotal: 0}
	tool, err := NewReadArticleTool(v)
	if err != nil {
		t.Fatalf("NewReadArticleTool: %v", err)
	}
	ctx := WithScope(context.Background(), Scope{UserID: 3, CategoryIDs: []int64{9}})
	raw, err := tool.InvokableRun(ctx, `{"article_id":20}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	var page readArticlePage
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		t.Fatalf("output phải JSON: %q: %v", raw, err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("bài ngoài scope không được trả chunk, got %d", len(page.Items))
	}
	if page.TotalChunk != 0 {
		t.Fatalf("GAP-3: đọc bài ngoài scope vẫn lộ total_chunk=%d (kỳ vọng 0 khi đếm có scope)", page.TotalChunk)
	}
	if v.countByArticleCalls != 0 {
		t.Fatalf("GAP-3: read_article còn gọi CountByArticle (unscoped) %d lần", v.countByArticleCalls)
	}
	if v.gotCountFilter.UserID != 3 || v.gotCountFilter.ArticleID != 20 ||
		len(v.gotCountFilter.CategoryIDs) != 1 || v.gotCountFilter.CategoryIDs[0] != 9 {
		t.Fatalf("GAP-3: CountByFilter phải nhận scope [9] + article 20, got %+v", v.gotCountFilter)
	}
}

// GAP-4 (regression): scope [9] — list_categories chỉ được trả chủ đề 9,
// không lộ 8/10 để agent gợi ý/đọc sai phạm vi.
func TestScopeGap4ListCategoriesRespectsScope(t *testing.T) {
	repo := &fakeScopedCategoryRepo{all: []*domain.Category{
		{BaseModel: domain.BaseModel{ID: 8}, Name: "ngoài scope"},
		{BaseModel: domain.BaseModel{ID: 9}, Name: "trong scope"},
		{BaseModel: domain.BaseModel{ID: 10}, Name: "ngoài scope 2"},
	}}
	tool, err := NewListCategoriesTool(repo)
	if err != nil {
		t.Fatalf("NewListCategoriesTool: %v", err)
	}
	ctx := WithScope(context.Background(), Scope{UserID: 3, CategoryIDs: []int64{9}})
	raw, err := tool.InvokableRun(ctx, `{}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	var got []listCategoriesItem
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("output phải JSON: %q: %v", raw, err)
	}
	if len(got) != 1 || got[0].ID != 9 {
		t.Fatalf("GAP-4: scope [9] nhưng list_categories trả %+v (kỳ vọng đúng chủ đề 9)", got)
	}
}
