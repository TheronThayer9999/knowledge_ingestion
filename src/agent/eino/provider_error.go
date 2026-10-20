package eino

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	openaiprovider "github.com/cloudwego/eino-ext/components/model/openai"
)

// ProviderHTTPError là lỗi HTTP backend LLM (OpenAI-compatible) đã bóc khỏi
// lớp wrapper của eino: client trả 404 cho model không tồn tại, 429 khi hết
// quota... Nếu để nguyên, caller chỉ thấy chuỗi NodeRunError dài và phải map
// thành 500 "internal server error" — người gọi API không biết mình gõ sai
// model hay provider đang chết.
type ProviderHTTPError struct {
	Status  int
	Message string
}

// providerStatusRe là dự phòng khi errors.As không xuyên qua được wrapper của
// eino (adk bọc lỗi node thành NodeRunError). Định dạng lấy từ
// openaiprovider.APIError.Error(): "status code: 404, status: 404 Not Found,
// message: ...".
var providerStatusRe = regexp.MustCompile(`status code: (\d{3}), status: [^,]*, message: ([^\n]*)`)

// AsProviderHTTPError rút status + message của backend LLM khỏi err. Trả false
// khi err không phải lỗi HTTP của provider (vd lỗi mạng, ctx timeout) — caller
// giữ nguyên đường xử lý cũ.
func AsProviderHTTPError(err error) (ProviderHTTPError, bool) {
	if err == nil {
		return ProviderHTTPError{}, false
	}
	// Đường chuẩn: chuỗi lỗi còn giữ *openaiprovider.APIError (eino wrap %w).
	var apiErr *openaiprovider.APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatusCode > 0 {
		return ProviderHTTPError{Status: apiErr.HTTPStatusCode, Message: strings.TrimSpace(apiErr.Message)}, true
	}
	// Đường dự phòng: wrapper của eino cắt đứt chuỗi Unwrap nên chỉ còn text.
	// Message cắt ở newline đầu để không kéo theo "node path: ..." của eino.
	if m := providerStatusRe.FindStringSubmatch(err.Error()); m != nil {
		status, convErr := strconv.Atoi(m[1])
		if convErr != nil {
			return ProviderHTTPError{}, false
		}
		return ProviderHTTPError{Status: status, Message: strings.TrimSpace(m[2])}, true
	}
	return ProviderHTTPError{}, false
}
