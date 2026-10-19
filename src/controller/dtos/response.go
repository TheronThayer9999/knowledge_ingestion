package dtos

import "knowledge_ingestion/src/common/errors"

// ResponseResource là envelope DUY NHẤT mọi endpoint trả về — kể cả lỗi.
// Data CỐ Ý không dùng omitempty: nếu có, response lỗi rơi còn 3 field còn
// response thành công có 4 field, client phải dò key mới biết có data hay
// không. Giữ luôn 4 field (data = null khi rỗng) thì client đọc
// resp.data thẳng không cần kiểm tra. Đổi lại: body lỗi dài thêm 11 ký tự
// — không đáng để đổi cấu trúc contract giữa các endpoint.
type ResponseResource struct {
	Status  bool   `json:"status" example:"true"`
	Code    int    `json:"code" example:"0"`
	Message string `json:"message" example:"success"`
	Data    any    `json:"data"`
}

func Success(data any) ResponseResource {
	return ResponseResource{
		Status:  true,
		Code:    errors.Success,
		Message: "success",
		Data:    data,
	}
}
