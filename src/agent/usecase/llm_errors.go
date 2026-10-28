package usecase

import (
	"fmt"
	"net/http"
	"strings"

	"knowledge_ingestion/src/common/errors"
)

// Hai lỗi dùng chung cho mọi đường vào của màn Nhà cung cấp AI (catalog, service,
// CRUD) — đặt riêng một file để các file nghiệp vụ không lẫn định nghĩa lỗi.

// unknownProvider là 404 "slug không có ở config lẫn bảng cài đặt" — admin gõ sai
// hay FE giữ danh sách cũ, không phải lỗi server.
func unknownProvider(slug string) *errors.Error {
	return errors.NewCustomHttpError(http.StatusNotFound, http.StatusNotFound,
		fmt.Sprintf("provider %q không có trong cấu hình", strings.TrimSpace(slug)))
}

// badLLMRequest là 400 đọc được cho form admin — lỗi nghiệp vụ, không phải sự cố
// server (để lỗi trần đi qua thì Render log stack và client nhận 500 vô nghĩa).
func badLLMRequest(msg string) *errors.Error {
	return errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, msg)
}
