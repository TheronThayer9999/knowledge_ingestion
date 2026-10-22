package domain

import "context"

// ArticleSection là 1 mục của bài đã compile (P2.4 phase 1b) — tên mục +
// tọa độ chunk lấy từ Qdrant ListSections (ground-truth), summary do LLM
// sinh và lưu lại để API/tree/agent đọc không tốn LLM lần hai.
//
// Khóa theo (article_id, name): ListSections khử trùng theo tên nên mỗi tên
// chỉ 1 dòng; compile ghi lại cả bài (Replace) nên đổi chunker/tên mục do
// index lại không để dòng cũ mồ côi.
type ArticleSection struct {
	BaseModel
	ArticleID  int64  `json:"article_id" gorm:"not null;uniqueIndex:idx_article_sections_key,priority:1"`
	Name       string `json:"name" gorm:"size:500;not null;uniqueIndex:idx_article_sections_key,priority:2"`
	StartChunk int    `json:"start_chunk" gorm:"not null;default:0"`
	Summary    string `json:"summary" gorm:"type:text;not null;default:''"`
}

func (ArticleSection) TableName() string {
	return "article_sections"
}

// IArticleSectionRepository hợp đồng bảng article_sections — compile service
// ghi, article service đọc merge vào PageIndex, purge dọn theo bài.
type IArticleSectionRepository interface {
	// Replace xóa dòng cũ của bài rồi ghi lô mới trong 1 transaction —
	// compile chạy lại không để trạng thái nửa vời (nửa cũ nửa mới).
	Replace(ctx context.Context, articleID int64, sections []*ArticleSection) error
	// ListByArticle trả mục của bài theo start_chunk tăng dần (thứ tự đọc).
	ListByArticle(ctx context.Context, articleID int64) ([]*ArticleSection, error)
	// DeleteByArticles xóa dòng của nhiều bài 1 câu — purge gom id đã dọn
	// xong blob + vector rồi xóa loạt, cùng pattern HardDeleteMany.
	DeleteByArticles(ctx context.Context, articleIDs []int64) error
}
