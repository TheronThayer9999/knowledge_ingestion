package dtos

import (
	"knowledge_ingestion/src/domain"
	"time"
)

// Articles là tài liệu tri thức của từng user — owner lấy từ token,
// không nhận từ client. Mỗi article có đúng 1 nguồn: URL (link web bên
// ngoài) HOẶC StorageKey (object đã upload lên kho qua presign) — service
// bắt đúng 1, còn đây chỉ validate hình thức từng field.

// CreateArticleRequest tạo bài viết mới trong một danh mục của mình.
type CreateArticleRequest struct {
	URL         string `json:"url,omitempty" binding:"omitempty,url,max=2048" example:"https://example.com/docs/go-fx"`
	StorageKey  string `json:"storage_key,omitempty" binding:"omitempty,max=500" example:"uploads/550e8400-e29b-41d4-a716-446655440000.pdf"`
	Name        string `json:"name" binding:"required,min=1,max=255" example:"Giới thiệu Go Fx"`
	ContentType string `json:"content_type" binding:"required,max=100" example:"text/html"`
	Description string `json:"description,omitempty" binding:"omitempty,max=1000" example:"Tài liệu về dependency injection trong Go"`
	CategoryID  int64  `json:"category_id" binding:"required,gt=0" example:"1"`
}

// UpdateArticleRequest sửa từng phần — field nil nghĩa là "không gửi",
// giữ nguyên giá trị cũ. Nguồn (URL/StorageKey) là identity nên không cho
// sửa — muốn đổi nguồn thì xóa tạo lại.
type UpdateArticleRequest struct {
	Name        *string `json:"name,omitempty" binding:"omitempty,min=1,max=255" example:"Giới thiệu Go Fx"`
	ContentType *string `json:"content_type,omitempty" binding:"omitempty,max=100" example:"text/html"`
	Description *string `json:"description,omitempty" binding:"omitempty,max=1000" example:"Tài liệu về dependency injection trong Go"`
	CategoryID  *int64  `json:"category_id,omitempty" binding:"omitempty,gt=0" example:"2"`
}

// ListArticlesQuery hứng query ?category_id=&category_name=&limit=&offset=
// bằng tag `form` của gin — lọc theo đúng 1 trong category_id hoặc
// category_name, luật "đúng 1" do service bắt (cross-field).
type ListArticlesQuery struct {
	CategoryID   int64  `form:"category_id" binding:"omitempty,gt=0"`
	CategoryName string `form:"category_name" binding:"omitempty,max=100"`
	Limit        int    `form:"limit" binding:"omitempty,gte=0"`
	Offset       int    `form:"offset" binding:"omitempty,gte=0"`
}

// ArticleResponse trả về cho client — không bao giờ lộ field nội bộ.
type ArticleResponse struct {
	ID          int64     `json:"id" example:"1"`
	UserID      int64     `json:"user_id" example:"7"`
	URL         string    `json:"url,omitempty" example:"https://example.com/docs/go-fx"`
	StorageKey  string    `json:"storage_key,omitempty" example:"uploads/550e8400-e29b-41d4-a716-446655440000.pdf"`
	Name        string    `json:"name" example:"Giới thiệu Go Fx"`
	ContentType string    `json:"content_type" example:"text/html"`
	Description string    `json:"description,omitempty" example:"Tài liệu về dependency injection trong Go"`
	CategoryID  int64     `json:"category_id" example:"1"`
	CreatedAt   time.Time `json:"created_at" example:"2026-10-06T10:00:00+07:00"`
	UpdatedAt   time.Time `json:"updated_at" example:"2026-10-06T10:00:00+07:00"`
}

// ArticleListResponse bọc danh sách + tổng số.
type ArticleListResponse struct {
	Articles []*ArticleResponse `json:"articles"`
	Total    int64              `json:"total" example:"10"`
}

// ToModel map request -> domain model — UserID do service gán từ token,
// không tin client. Nguồn (URL hay StorageKey) service đã chốt đúng 1
// trước khi tới đây.
func (r *CreateArticleRequest) ToModel() *domain.Article {
	return &domain.Article{
		URL:         r.URL,
		StorageKey:  r.StorageKey,
		Name:        r.Name,
		ContentType: r.ContentType,
		Description: r.Description,
		CategoryID:  r.CategoryID,
	}
}

// ToArticleResponse map domain -> response.
func ToArticleResponse(a *domain.Article) *ArticleResponse {
	if a == nil {
		return &ArticleResponse{}
	}
	return &ArticleResponse{
		ID:          a.ID,
		UserID:      a.UserID,
		URL:         a.URL,
		StorageKey:  a.StorageKey,
		Name:        a.Name,
		ContentType: a.ContentType,
		Description: a.Description,
		CategoryID:  a.CategoryID,
		CreatedAt:   a.CreatedAt,
		UpdatedAt:   a.UpdatedAt,
	}
}

// ToArticleListResponse map danh sách domain -> response.
func ToArticleListResponse(articles []*domain.Article) ArticleListResponse {
	res := ArticleListResponse{
		Articles: make([]*ArticleResponse, 0, len(articles)),
		Total:    int64(len(articles)),
	}
	for _, a := range articles {
		res.Articles = append(res.Articles, ToArticleResponse(a))
	}
	return res
}

// SearchRequest hỏi tri thức — query bắt buộc, category_id để khoanh 1 chủ
// đề, limit/threshold để kiểm soát lượng + chất hit. Owner lấy từ token.
// Model là model LLM cho vòng 1 agentic (phân loại câu hỏi) — rỗng thì default
// lookup, khỏi tốn lượt LLM.
type SearchRequest struct {
	Query          string  `json:"query" binding:"required,min=1,max=2000" example:"Go Fx quản lý vòng đời thế nào"`
	CategoryID     int64   `json:"category_id,omitempty" binding:"omitempty,gt=0" example:"5"`
	Limit          int     `json:"limit,omitempty" binding:"omitempty,gte=0"`
	ScoreThreshold float32 `json:"score_threshold,omitempty" binding:"omitempty,gte=0"`
	Model          string  `json:"model,omitempty" example:"gemma4:31b"`
}

// SearchHitResponse là 1 đoạn trúng — agent RAG lấy text làm ngữ cảnh,
// page_num/chunk_index để trích dẫn, score để cân nhắc độ tin.
type SearchHitResponse struct {
	ArticleID  int64   `json:"article_id" example:"8"`
	ChunkIndex int     `json:"chunk_index" example:"11"`
	PageNum    int     `json:"page_num" example:"0"`
	Score      float32 `json:"score" example:"0.83"`
	Text       string  `json:"text" example:"Chất lượng là ưu tiên hàng đầu..."`
}

// SearchResponse bọc danh sách hit + tổng số + loại câu hỏi vòng 1 (để agent
// biết đường tổng hợp tiếp).
type SearchResponse struct {
	Hits         []*SearchHitResponse `json:"hits"`
	Total        int                  `json:"total" example:"3"`
	QuestionType string               `json:"question_type" example:"compare"`
}

// ToSearchHitResponse map domain -> response.
func ToSearchHitResponse(h *domain.ScoredChunk) *SearchHitResponse {
	if h == nil {
		return &SearchHitResponse{}
	}
	return &SearchHitResponse{
		ArticleID:  h.ArticleID,
		ChunkIndex: h.ChunkIndex,
		PageNum:    h.PageNum,
		Score:      h.Score,
		Text:       h.Text,
	}
}

// RebuildVectorsRequest đưa bài về hàng đợi chunk→embed từ đầu (Qdrant
// chết/mất collection, chuyển cụm mới). Scope bắt buộc đúng 1 trong 2:
// article_ids hoặc category_id — cả 2 rỗng service trả 400 để không reset
// nhầm toàn bộ. Chỉ tác động bài của chính mình (owner từ token).
type RebuildVectorsRequest struct {
	ArticleIDs []int64 `json:"article_ids,omitempty" binding:"omitempty,dive,gt=0"`
	CategoryID int64   `json:"category_id,omitempty" binding:"omitempty,gt=0" example:"5"`
}

// RebuildVectorsResponse báo số bài đã đưa về pending — worker hốt dần theo
// nhịp claim, theo dõi qua GET /articles như bình thường.
type RebuildVectorsResponse struct {
	ResetArticles int64   `json:"reset_articles" example:"2"`
	ArticleIDs    []int64 `json:"article_ids"`
}
