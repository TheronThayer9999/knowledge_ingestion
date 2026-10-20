package usecase

import (
	"fmt"
	"net/http"

	"knowledge_ingestion/src/agent/eino"
	"knowledge_ingestion/src/common/errors"
)

// providerError đổi lỗi backend LLM thành lỗi HTTP đọc được thay vì 500
// "internal server error". Model không tồn tại/không được hỗ trợ (400, 404,
// 422) là lỗi phía request nên trả 400 kèm message của provider — giữ nguyên
// 500 thì người gọi API không biết mình gõ sai model hay provider đang chết.
// Hết quota/tốc độ (429) trả 429; còn lại (401 key sai, 5xx) là sự cố cấu
// hình/backend phía server → 502, không phải lỗi của client.
//
// Lỗi không phải HTTP của provider (mạng, ctx timeout, parse) trả nguyên để
// Render log stack và map 500 như cũ.
func providerError(err error, model string) error {
	pe, ok := eino.AsProviderHTTPError(err)
	if !ok {
		return err
	}
	switch pe.Status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusUnprocessableEntity:
		return errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest,
			fmt.Sprintf("provider từ chối model %q: %s", model, pe.Message))
	case http.StatusTooManyRequests:
		return errors.NewCustomHttpError(http.StatusTooManyRequests, http.StatusTooManyRequests,
			fmt.Sprintf("provider giới hạn tốc độ/hết quota với model %q: %s", model, pe.Message))
	default:
		return errors.NewCustomHttpError(http.StatusBadGateway, http.StatusBadGateway,
			fmt.Sprintf("provider lỗi %d với model %q: %s", pe.Status, model, pe.Message))
	}
}
