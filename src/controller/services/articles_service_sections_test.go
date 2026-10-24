package services

import (
	"context"
	stderrors "errors"
	cerr "knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
	"net/http"
	"testing"
)

// sectionsStubArticles là repo giả cho test ListSections — chỉ GetByID được
// cài; method khác không implement (nhúng interface, gọi nhầm là panic),
// nên test khoá đúng đường đi của service.
type sectionsStubArticles struct {
	domain.IArticleRepository
	article *domain.Article
	err     error
	gotID   int64
	gotUser int64
}

func (s *sectionsStubArticles) GetByID(_ context.Context, id int64, userID int64) (*domain.Article, error) {
	s.gotID, s.gotUser = id, userID
	return s.article, s.err
}

// sectionsStubVectors chỉ ListSections có dữ liệu (nhúng interface cho đủ
// hợp đồng IVectorStore — method khác gọi nhầm là panic); bắt cả filter để
// khoá quyền: UserID phải là owner từ token, ArticleID là bài yêu cầu.
type sectionsStubVectors struct {
	domain.IVectorStore
	sections []domain.SectionSpan
	err      error
	got      domain.SearchFilter
	called   bool
}

func (v *sectionsStubVectors) ListSections(_ context.Context, filter domain.SearchFilter) ([]domain.SectionSpan, error) {
	v.got = filter
	v.called = true
	return v.sections, v.err
}

// stubUser trả user_id cố định — route đã qua auth middleware.
type stubUser struct{ id int64 }

func (u stubUser) UserID(context.Context) (int64, bool) { return u.id, true }

// sectionsStubRepo chỉ ListByArticle có dữ liệu (nhúng interface cho đủ hợp
// đồng — method khác gọi nhầm là panic) — test khoá bài chưa compile KHÔNG
// được chạm bảng article_sections.
type sectionsStubRepo struct {
	domain.IArticleSectionRepository
	rows   []*domain.ArticleSection
	err    error
	called bool
}

func (r *sectionsStubRepo) ListByArticle(_ context.Context, _ int64) ([]*domain.ArticleSection, error) {
	r.called = true
	return r.rows, r.err
}

func newSectionsSvc(repo *sectionsStubArticles, vectors *sectionsStubVectors, sectionRepo domain.IArticleSectionRepository) IArticleService {
	// heartbeatRepo nil: các test này chỉ chạm ListSections/GetTree, không
	// gọi ListStatuses nên không cần nhịp sống worker.
	return NewArticleService(repo, nil, stubUser{id: 7}, nil, vectors, sectionRepo, nil)
}

func TestArticleListSectionsReturnsPageIndex(t *testing.T) {
	repo := &sectionsStubArticles{article: &domain.Article{
		BaseModel:   domain.BaseModel{ID: 8},
		Name:        "Quy định nội bộ",
		IndexStatus: domain.QueueDone,
	}}
	vectors := &sectionsStubVectors{sections: []domain.SectionSpan{
		{Name: "ĐIỀU 5: CHẾ ĐỘ ĐÓNG BẢO HIỂM", From: 40},
		{Name: "ĐIỀU 6: HIỆU LỰC", From: 61},
	}}
	res := newSectionsSvc(repo, vectors, &sectionsStubRepo{}).ListSections(context.Background(), 8)
	if res.Err != nil {
		t.Fatalf("ListSections: %v", res.Err)
	}
	if res.Data.ArticleID != 8 || res.Data.ArticleName != "Quy định nội bộ" || res.Data.IndexStatus != domain.QueueDone {
		t.Errorf("metadata bài sai: %+v", res.Data)
	}
	if res.Data.SectionCount != 2 || len(res.Data.Sections) != 2 {
		t.Fatalf("section_count = %d, muốn 2", res.Data.SectionCount)
	}
	if got := res.Data.Sections[0]; got.Name != "ĐIỀU 5: CHẾ ĐỘ ĐÓNG BẢO HIỂM" || got.From != 40 {
		t.Errorf("mục đầu sai: %+v", got)
	}
	// Khóa phân quyền: filter mang owner từ token + article id yêu cầu.
	if vectors.got.UserID != 7 || vectors.got.ArticleID != 8 {
		t.Errorf("filter Qdrant sai quyền: %+v", vectors.got)
	}
	if repo.gotID != 8 || repo.gotUser != 7 {
		t.Errorf("repo load bài sai owner: id=%d user=%d", repo.gotID, repo.gotUser)
	}
}

func TestArticleListSectionsEmptyIsNotNil(t *testing.T) {
	// Bài chưa index xong: sections rỗng (mảng, không null) + index_status
	// để client tự phân biệt, không phải lỗi.
	repo := &sectionsStubArticles{article: &domain.Article{
		BaseModel:   domain.BaseModel{ID: 3},
		IndexStatus: domain.QueuePending,
	}}
	res := newSectionsSvc(repo, &sectionsStubVectors{}, &sectionsStubRepo{}).ListSections(context.Background(), 3)
	if res.Err != nil {
		t.Fatalf("ListSections: %v", res.Err)
	}
	if res.Data.Sections == nil {
		t.Fatal("sections phải là mảng rỗng, không nil")
	}
	if len(res.Data.Sections) != 0 || res.Data.SectionCount != 0 || res.Data.IndexStatus != domain.QueuePending {
		t.Errorf("payload rỗng sai: %+v", res.Data)
	}
}

func TestArticleListSectionsForeignArticleIs404(t *testing.T) {
	// Bài của người khác: repo scope owner trả ErrNotFound → 404, và TUYỆT
	// ĐỐI không được chạm Qdrant (filter không đổi → zero value).
	vectors := &sectionsStubVectors{}
	svc := newSectionsSvc(&sectionsStubArticles{err: domain.ErrNotFound}, vectors, &sectionsStubRepo{})
	res := svc.ListSections(context.Background(), 99)
	if res.Err == nil {
		t.Fatal("muốn 404, nhận ok")
	}
	e := cerr.From(res.Err)
	if e == nil || e.GetHttpCode() != http.StatusNotFound {
		t.Fatalf("không phải 404: %v", res.Err)
	}
	if vectors.called {
		t.Errorf("bài không thuộc owner mà vẫn hỏi Qdrant: %+v", vectors.got)
	}
}

func TestArticleListSectionsVectorErrorPropagates(t *testing.T) {
	boom := stderrors.New("qdrant down")
	res := newSectionsSvc(
		&sectionsStubArticles{article: &domain.Article{BaseModel: domain.BaseModel{ID: 1}}},
		&sectionsStubVectors{err: boom},
		&sectionsStubRepo{},
	).ListSections(context.Background(), 1)
	if res.Err == nil || !stderrors.Is(res.Err, boom) {
		t.Fatalf("lỗi Qdrant phải nổi lên (Render map thành 500), nhận: %v", res.Err)
	}
}

func TestArticleListSectionsMergesSummaries(t *testing.T) {
	// Bài đã compile done: summary đọc từ article_sections merge theo tên mục,
	// mục không có dòng trong bảng (mới index thêm) thì summary rỗng.
	repo := &sectionsStubArticles{article: &domain.Article{
		BaseModel:     domain.BaseModel{ID: 8},
		IndexStatus:   domain.QueueDone,
		CompileStatus: domain.QueueDone,
	}}
	vectors := &sectionsStubVectors{sections: []domain.SectionSpan{
		{Name: "ĐIỀU 5: CHẾ ĐỘ ĐÓNG BẢO HIỂM", From: 40},
		{Name: "ĐIỀU 6: HIỆU LỰC", From: 61},
	}}
	sectionRepo := &sectionsStubRepo{rows: []*domain.ArticleSection{
		{Name: "ĐIỀU 5: CHẾ ĐỘ ĐÓNG BẢO HIỂM", Summary: "Mức và thời hạn đóng BHXH."},
	}}
	res := newSectionsSvc(repo, vectors, sectionRepo).ListSections(context.Background(), 8)
	if res.Err != nil {
		t.Fatalf("ListSections: %v", res.Err)
	}
	if res.Data.CompileStatus != domain.QueueDone {
		t.Errorf("compile_status sai: %q", res.Data.CompileStatus)
	}
	if got := res.Data.Sections[0]; got.Summary != "Mức và thời hạn đóng BHXH." {
		t.Errorf("summary chưa merge: %+v", got)
	}
	if res.Data.Sections[1].Summary != "" {
		t.Errorf("mục chưa compile phải có summary rỗng: %+v", res.Data.Sections[1])
	}
	if !sectionRepo.called {
		t.Error("bài done thì phải đọc article_sections")
	}
}

func TestArticleListSectionsNotCompiledSkipsSectionRepo(t *testing.T) {
	// Bài mới index, chưa compile: KHÔNG được chạm bảng article_sections
	// (map rỗng) — client phân biệt qua compile_status, không phải lỗi.
	repo := &sectionsStubArticles{article: &domain.Article{
		BaseModel:     domain.BaseModel{ID: 8},
		IndexStatus:   domain.QueueDone,
		CompileStatus: domain.QueuePending,
	}}
	vectors := &sectionsStubVectors{sections: []domain.SectionSpan{{Name: "ĐIỀU 1", From: 0}}}
	sectionRepo := &sectionsStubRepo{}
	res := newSectionsSvc(repo, vectors, sectionRepo).ListSections(context.Background(), 8)
	if res.Err != nil {
		t.Fatalf("ListSections: %v", res.Err)
	}
	if sectionRepo.called {
		t.Error("bài chưa compile mà vẫn đọc article_sections")
	}
	if res.Data.Sections[0].Summary != "" || res.Data.CompileStatus != domain.QueuePending {
		t.Errorf("payload sai: %+v", res.Data)
	}
}

// ---------- Phase 1c: cây mục (BuildArticleTree + GetTree) ----------

func TestBuildArticleTreeNestsByHeading(t *testing.T) {
	// Phân cấp heading của chunker: Chương/Phần = 1, Mục = 2, Điều/Bài = 3;
	// tên tự do (không headword) coi là lá treo dưới cha gần nhất.
	sections := []domain.SectionSpan{
		{Name: "PHẦN A: QUY ĐỊNH CHUNG", From: 0},
		{Name: "CHƯƠNG I: CHẾ ĐỘ"},
		{Name: "MỤC 1: BẢO HIỂM", From: 10},
		{Name: "ĐIỀU 1: PHẠM VI", From: 11},
		{Name: "ĐIỀU 2: ĐỐI TƯỢNG", From: 15},
		{Name: "CHƯƠNG II: HIỆU LỰC", From: 20},
		{Name: "ĐIỀU 3: TỔ CHỨC THỰC HIỆN", From: 21},
		{Name: "PHỤ LỤC", From: 30},
	}
	sections[1].From = 5
	res := dtos.BuildArticleTree(&domain.Article{BaseModel: domain.BaseModel{ID: 1}}, sections, map[string]string{})
	if res.NodeCount != 8 {
		t.Fatalf("node_count = %d, muốn 8", res.NodeCount)
	}
	if len(res.Roots) != 3 || res.Roots[0].Name != "PHẦN A: QUY ĐỊNH CHUNG" {
		t.Fatalf("roots sai: %v", rootNames(res.Roots))
	}
	chuongI := res.Roots[1]
	if len(chuongI.Children) != 1 || chuongI.Children[0].Name != "MỤC 1: BẢO HIỂM" {
		t.Fatalf("CHƯƠNG I phải có đúng con MỤC 1: %v", chuongI.Children)
	}
	if len(chuongI.Children[0].Children) != 2 {
		t.Errorf("MỤC 1 phải có 2 Điều, nhận %d", len(chuongI.Children[0].Children))
	}
	chuongII := res.Roots[2]
	// ĐIỀU 3 + PHỤ LỤC (lá không headword) đều treo dưới CHƯƠNG II.
	if len(chuongII.Children) != 2 || chuongII.Children[1].Name != "PHỤ LỤC" {
		t.Errorf("CHƯƠNG II con sai: %v", chuongII.Children)
	}
	if chuongII.Children[0].Children == nil {
		t.Error("children phải là mảng rỗng, không null (JSON)")
	}
}

func TestBuildArticleTreeFlatWhenNoChapters(t *testing.T) {
	// Bài chỉ có Điều (như tài liệu test hiện tại): mọi mục là root — cây
	// phẳng, không bịa ra cha ảo.
	sections := []domain.SectionSpan{
		{Name: "ĐIỀU 1: A", From: 0},
		{Name: "ĐIỀU 2: B", From: 5},
		{Name: "ĐIỀU 3: C", From: 9},
	}
	res := dtos.BuildArticleTree(nil, sections, nil)
	if len(res.Roots) != 3 || res.NodeCount != 3 {
		t.Fatalf("muốn 3 root phẳng, nhận %d/%d", len(res.Roots), res.NodeCount)
	}
	for _, r := range res.Roots {
		if len(r.Children) != 0 {
			t.Errorf("root %q không được có con", r.Name)
		}
	}
}

func TestArticleGetTreeNestsWithSummaries(t *testing.T) {
	// GetTree đi đúng nguồn với ListSections: owner check + filter từ token +
	// merge summary khi bài done, rồi mới nest.
	repo := &sectionsStubArticles{article: &domain.Article{
		BaseModel:     domain.BaseModel{ID: 8},
		IndexStatus:   domain.QueueDone,
		CompileStatus: domain.QueueDone,
	}}
	vectors := &sectionsStubVectors{sections: []domain.SectionSpan{
		{Name: "CHƯƠNG I: CHẾ ĐỘ", From: 0},
		{Name: "ĐIỀU 1: PHẠM VI", From: 1},
		{Name: "ĐIỀU 2: ĐỐI TƯỢNG", From: 9},
	}}
	sectionRepo := &sectionsStubRepo{rows: []*domain.ArticleSection{
		{Name: "CHƯƠNG I: CHẾ ĐỘ", Summary: "Chương gộp 2 điều đầu."},
		{Name: "ĐIỀU 1: PHẠM VI", Summary: "Phạm vi áp dụng toàn công ty."},
	}}
	res := newSectionsSvc(repo, vectors, sectionRepo).GetTree(context.Background(), 8)
	if res.Err != nil {
		t.Fatalf("GetTree: %v", res.Err)
	}
	if res.Data.NodeCount != 3 || len(res.Data.Roots) != 1 {
		t.Fatalf("cây sai: nodes=%d roots=%d", res.Data.NodeCount, len(res.Data.Roots))
	}
	root := res.Data.Roots[0]
	if root.Name != "CHƯƠNG I: CHẾ ĐỘ" || root.Summary != "Chương gộp 2 điều đầu." {
		t.Errorf("root sai: %+v", root)
	}
	if len(root.Children) != 2 || root.Children[0].Summary != "Phạm vi áp dụng toàn công ty." {
		t.Errorf("con sai: %+v", root.Children)
	}
	if root.Children[1].Summary != "" {
		t.Errorf("mục thiếu dòng trong bảng phải có summary rỗng")
	}
	if vectors.got.UserID != 7 || vectors.got.ArticleID != 8 {
		t.Errorf("filter Qdrant sai quyền: %+v", vectors.got)
	}
}

func TestArticleGetTreeForeignArticleIs404(t *testing.T) {
	vectors := &sectionsStubVectors{}
	res := newSectionsSvc(&sectionsStubArticles{err: domain.ErrNotFound}, vectors, &sectionsStubRepo{}).
		GetTree(context.Background(), 99)
	if res.Err == nil {
		t.Fatal("muốn 404, nhận ok")
	}
	if e := cerr.From(res.Err); e == nil || e.GetHttpCode() != http.StatusNotFound {
		t.Fatalf("không phải 404: %v", res.Err)
	}
	if vectors.called {
		t.Error("bài không thuộc owner mà vẫn hỏi Qdrant")
	}
}

func rootNames(nodes []*dtos.SectionTreeNodeResponse) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Name)
	}
	return out
}
