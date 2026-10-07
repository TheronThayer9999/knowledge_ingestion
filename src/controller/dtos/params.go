package dtos

// IDParam hứng :id trên path cho mọi resource (articles, categories...)
// bằng tag `uri` của gin — dùng chung một chỗ thay vì mỗi controller
// định nghĩa struct riêng giống hệt nhau.
type IDParam struct {
	ID int64 `uri:"id" binding:"required,gt=0" example:"1"`
}
