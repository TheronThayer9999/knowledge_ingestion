package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"
	"knowledge_ingestion/src/infrastructure/postgres"

	"gorm.io/gorm"
)

// newPageIssuesTestDB mở DB thật từ config; thử config.local.json trước (máy
// dev chạy Docker publish 5432) rồi config.json (host trong Docker network),
// không reachable thì skip để máy không bật Docker vẫn xanh.
func newPageIssuesTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	paths := []string{os.Getenv("APP_CONFIG_PATH")}
	for _, name := range []string{"config.local.json", "config.json"} {
		paths = append(paths, filepath.Join("..", "..", "..", "configs", name))
	}
	for _, path := range paths {
		if path == "" {
			continue
		}
		cfg, err := config.Load(path)
		if err != nil {
			continue
		}
		conn, err := postgres.NewConnection(cfg)
		if err != nil {
			continue
		}
		return conn.GetDB()
	}
	t.Skip("postgres không reachable — bỏ qua test cần DB")
	return nil
}

// TestArticlePageRepositoryListPageIssues khoá đúng định nghĩa "trang không
// đọc được": rỗng, toàn khoảng trắng, ngắn hơn ngưỡng; và khoá cả 2 cái bẫy
// đã gặp — đếm theo BYTE (tiếng Việt) và btrim chỉ cắt dấu cách.
func TestArticlePageRepositoryListPageIssues(t *testing.T) {
	db := newPageIssuesTestDB(t)
	repo := NewArticlePageIssues(db)
	ctx := context.Background()

	// Không cần row article: bảng không có FK, id tổng hợp là đủ và không
	// đụng dữ liệu thật.
	articleID := time.Now().UnixNano()
	t.Cleanup(func() {
		db.Unscoped().Where("article_id = ?", articleID).Delete(&domain.ArticlePage{})
	})

	pages := []*domain.ArticlePage{
		{Seq: 1, ArticleID: articleID, PageNum: 1, Text: "Trang này có nội dung đầy đủ, dài hơn ngưỡng bốn mươi ký tự."},
		{Seq: 2, ArticleID: articleID, PageNum: 2, Text: ""},
		{Seq: 3, ArticleID: articleID, PageNum: 3, Text: " \n\t \r\n "},
		{Seq: 4, ArticleID: articleID, PageNum: 4, Text: "Chương II", LayoutScore: 0.55},
		{Seq: 5, ArticleID: articleID, PageNum: 5, Text: "Ngắn"},
		{Seq: 6, ArticleID: articleID, PageNum: 6, Text: "Đã xoá mềm"}, // 11 ký tự nhưng phải bị bỏ qua
	}
	if err := NewArticlePageRepository(db).CreateBatch(ctx, pages); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}
	// Xoá mềm 1 trang ngắn: không được xuất hiện trong kết quả.
	if err := db.Where("article_id = ? AND seq = ?", articleID, 6).Delete(&domain.ArticlePage{}).Error; err != nil {
		t.Fatalf("xoá mềm: %v", err)
	}

	issues, err := repo.ListPageIssues(ctx, articleID, 40)
	if err != nil {
		t.Fatalf("ListPageIssues: %v", err)
	}
	var seqs []int
	for _, is := range issues {
		seqs = append(seqs, is.Seq)
	}
	if len(seqs) != 4 || seqs[0] != 2 || seqs[1] != 3 || seqs[2] != 4 || seqs[3] != 5 {
		t.Fatalf("phải trả seq 2,3,4,5 theo thứ tự đọc (bỏ trang xoá mềm 6), got %v", seqs)
	}
	if issues[0].Chars != 0 || issues[1].Chars != 0 {
		t.Fatalf("trang rỗng/toàn khoảng trắng phải đếm 0 ký tự, got %d/%d", issues[0].Chars, issues[1].Chars)
	}
	// 4 ký tự thật, KHÔNG phải 5 byte (ắ = 3 byte) — bắt lỗi đếm theo byte.
	if issues[3].Chars != 4 {
		t.Fatalf("\"Ngắn\" phải là 4 ký tự (không phải byte), got %d", issues[3].Chars)
	}
	if issues[2].LayoutScore != 0.55 {
		t.Fatalf("layout_score phải đọc lại được, got %v", issues[2].LayoutScore)
	}

	// Ngưỡng thấp thì chỉ còn trang thật sự trắng.
	strict, err := repo.ListPageIssues(ctx, articleID, 1)
	if err != nil {
		t.Fatalf("ListPageIssues(minChars=1): %v", err)
	}
	if len(strict) != 2 || strict[0].Seq != 2 || strict[1].Seq != 3 {
		t.Fatalf("ngưỡng 1 chỉ được bắt trang 0 ký tự, got %+v", strict)
	}

	// Bài không có trang nào: rỗng, không lỗi (tool cần phân biệt "sạch" với
	// "lỗi DB").
	empty, err := repo.ListPageIssues(ctx, -1, 40)
	if err != nil {
		t.Fatalf("ListPageIssues(bài không tồn tại): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("bài không có trang phải trả rỗng, got %d", len(empty))
	}
}
