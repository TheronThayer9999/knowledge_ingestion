package dtos

import (
	"knowledge_ingestion/src/domain"
	"time"
)

// Categories là taxonomy folder theo từng user — mỗi user có nhiều cây
// (nhiều root), owner lấy từ token, không nhận từ client.

// CreateCategoryRequest tạo danh mục mới. ParentID nil nghĩa là tạo root —
// user được nhiều root.
type CreateCategoryRequest struct {
	Name        string `json:"name" binding:"required,min=1,max=100" example:"Khoa học"`
	Description string `json:"description,omitempty" binding:"omitempty,max=500" example:"Tài liệu khoa học"`
	ParentID    *int64 `json:"parent_id,omitempty" example:"1"`
}

// UpdateCategoryRequest sửa từng phần — field nil nghĩa là "không gửi",
// giữ nguyên giá trị cũ. MoveToRoot=true đưa danh mục về root; client không
// thể diễn đạt "bỏ cha" bằng parent_id: null nên cần cờ riêng, gửi kèm
// parent_id là lỗi 400.
type UpdateCategoryRequest struct {
	Name        *string `json:"name,omitempty" binding:"omitempty,min=1,max=100" example:"Khoa học"`
	Description *string `json:"description,omitempty" binding:"omitempty,max=500" example:"Tài liệu khoa học"`
	ParentID    *int64  `json:"parent_id,omitempty" example:"1"`
	MoveToRoot  *bool   `json:"move_to_root,omitempty" example:"false"`
}

// CategoryResponse trả về cho client ở các thao tác mutation — không gồm vị
// trí trong cây vì row đã xoá thì placement không còn nghĩa.
type CategoryResponse struct {
	ID          int64     `json:"id" example:"1"`
	UserID      int64     `json:"user_id" example:"7"`
	Name        string    `json:"name" example:"Khoa học"`
	Description string    `json:"description,omitempty" example:"Tài liệu khoa học"`
	CreatedAt   time.Time `json:"created_at" example:"2026-10-06T10:00:00+07:00"`
	UpdatedAt   time.Time `json:"updated_at" example:"2026-10-06T10:00:00+07:00"`
}

// CategoryNodeResponse là category kèm vị trí trong cây: ParentID nil =
// root, Depth 0 = root. Client dựng cây từ danh sách phẳng bằng 2 field này.
type CategoryNodeResponse struct {
	ID          int64     `json:"id" example:"1"`
	UserID      int64     `json:"user_id" example:"7"`
	Name        string    `json:"name" example:"Khoa học"`
	Description string    `json:"description,omitempty" example:"Tài liệu khoa học"`
	ParentID    *int64    `json:"parent_id,omitempty" example:"1"`
	Depth       int       `json:"depth" example:"1"`
	CreatedAt   time.Time `json:"created_at" example:"2026-10-06T10:00:00+07:00"`
	UpdatedAt   time.Time `json:"updated_at" example:"2026-10-06T10:00:00+07:00"`
}

// CategoryListResponse bọc danh sách + tổng số.
type CategoryListResponse struct {
	Categories []*CategoryNodeResponse `json:"categories"`
	Total      int64                   `json:"total" example:"10"`
}

// ToCategoryResponse map domain -> response của thao tác mutation.
func ToCategoryResponse(c *domain.Category) *CategoryResponse {
	if c == nil {
		return &CategoryResponse{}
	}
	return &CategoryResponse{
		ID:          c.ID,
		UserID:      c.UserID,
		Name:        c.Name,
		Description: c.Description,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}
}

// ToCategoryNodeResponse map domain -> response kèm vị trí trong cây.
// parentID nil + depth 0 nghĩa là root.
func ToCategoryNodeResponse(c *domain.Category, parentID *int64, depth int) *CategoryNodeResponse {
	if c == nil {
		return &CategoryNodeResponse{}
	}
	return &CategoryNodeResponse{
		ID:          c.ID,
		UserID:      c.UserID,
		Name:        c.Name,
		Description: c.Description,
		ParentID:    parentID,
		Depth:       depth,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}
}

// ToCategoryListResponse map danh sách domain -> list node. parentOf và
// levelOf do service dựng từ closure: parentOf[id] = cha trực tiếp,
// levelOf[id] = cấp tuyệt đối (0 = root).
func ToCategoryListResponse(categories []*domain.Category, parentOf map[int64]int64, levelOf map[int64]int) CategoryListResponse {
	res := CategoryListResponse{
		Categories: make([]*CategoryNodeResponse, 0, len(categories)),
		Total:      int64(len(categories)),
	}
	for _, c := range categories {
		var parentID *int64
		if p, ok := parentOf[c.ID]; ok {
			pid := p
			parentID = &pid
		}
		res.Categories = append(res.Categories, ToCategoryNodeResponse(c, parentID, levelOf[c.ID]))
	}
	return res
}
