package dtos

import (
	"knowledge_ingestion/src/domain"
	"time"
)

// Categories là taxonomy theo từng user (mỗi user 1 cây single-root) —
// owner lấy từ token, không nhận từ client.

// CreateCategoryRequest tạo danh mục mới. ParentID nil nghĩa là tạo root
// (mỗi user chỉ 1 root — tạo root thứ hai sẽ 500 do unique index).
type CreateCategoryRequest struct {
	Name        string `json:"name" binding:"required,min=1,max=100" example:"Khoa học"`
	Description string `json:"description,omitempty" binding:"omitempty,max=500" example:"Tài liệu khoa học"`
	ParentID    *int64 `json:"parent_id,omitempty" example:"1"`
}

// UpdateCategoryRequest sửa từng phần — field nil nghĩa là "không gửi",
// giữ nguyên giá trị cũ.
type UpdateCategoryRequest struct {
	Name        *string `json:"name,omitempty" binding:"omitempty,min=1,max=100" example:"Khoa học"`
	Description *string `json:"description,omitempty" binding:"omitempty,max=500" example:"Tài liệu khoa học"`
	ParentID    *int64  `json:"parent_id,omitempty" example:"1"`
}

// CategoryResponse trả về cho client — không bao giờ lộ field nội bộ.
type CategoryResponse struct {
	ID          int64     `json:"id" example:"1"`
	UserID      int64     `json:"user_id" example:"7"`
	Name        string    `json:"name" example:"Khoa học"`
	Description string    `json:"description,omitempty" example:"Tài liệu khoa học"`
	ParentID    *int64    `json:"parent_id,omitempty" example:"1"`
	CreatedAt   time.Time `json:"created_at" example:"2026-10-06T10:00:00+07:00"`
	UpdatedAt   time.Time `json:"updated_at" example:"2026-10-06T10:00:00+07:00"`
}

// CategoryListResponse bọc danh sách + tổng số.
type CategoryListResponse struct {
	Categories []*CategoryResponse `json:"categories"`
	Total      int64               `json:"total" example:"10"`
}

// ToCategoryResponse map domain -> response.
func ToCategoryResponse(c *domain.Category) *CategoryResponse {
	if c == nil {
		return &CategoryResponse{}
	}
	return &CategoryResponse{
		ID:          c.ID,
		UserID:      c.UserID,
		Name:        c.Name,
		Description: c.Description,
		ParentID:    c.ParentID,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}
}

// ToCategoryListResponse map danh sách domain -> response.
func ToCategoryListResponse(categories []*domain.Category) CategoryListResponse {
	res := CategoryListResponse{
		Categories: make([]*CategoryResponse, 0, len(categories)),
		Total:      int64(len(categories)),
	}
	for _, c := range categories {
		res.Categories = append(res.Categories, ToCategoryResponse(c))
	}
	return res
}
