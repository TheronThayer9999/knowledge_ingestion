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
