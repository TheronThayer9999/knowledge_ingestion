package usecase

import (
	"context"
	stderrors "errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"knowledge_ingestion/src/agent/eino"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/logs"
)

// aiUnavailableMessage là thông báo client nhận khi dịch vụ AI nằm dưới chat/
// search (Ollama local để nhúng câu hỏi, vector DB) không kết nối được. 500
// "internal server error" không cho người dùng biết phải bật cái gì lên;
// ngược lại URL/cổng thật (dial tcp [::1]:11434) chỉ đi vào log, không lộ ra
// response.
const aiUnavailableMessage = "không kết nối được dịch vụ AI (Ollama embedding / tìm kiếm) — kiểm tra Ollama local và các container Docker đang chạy rồi thử lại"

// depDownMarkers nhận diện lỗi không kết nối được dịch vụ qua TEXT. Phải đọc
// text vì adk của eino bọc lỗi tool thành NodeRunError, chuỗi Unwrap đứt (y
// hệt lý do providerStatusRe bên eino). Windows không nói "connection refused"
// mà "connectex: No connection could be made because the target machine
// actively refused it" — cả hai kiểu đều phải bắt được.
var depDownMarkers = []string{
	"connection refused", "actively refused", "connectex", "no such host", "i/o timeout",
}

// dependencyError đổi lỗi không kết nối được hạ tầng sang 503, lỗi khác trả nil
// để caller giữ nguyên đường 500 + log stack. Client chủ động ngắt (nút Dừng
// trên FE, ctx bị hủy) thì KHÔNG đổi — dịch vụ vẫn sống, báo hạ tầng chết là
// sai và còn khiến người dùng đi sửa nhầm thứ.
func dependencyError(err error) error {
	if err == nil || stderrors.Is(err, context.Canceled) {
		return nil
	}
	var opErr *net.OpError
	down := stderrors.As(err, &opErr)
	if !down {
		msg := strings.ToLower(err.Error())
		for _, marker := range depDownMarkers {
			if strings.Contains(msg, marker) {
				down = true
				break
			}
		}
	}
	if !down {
		return nil
	}
	// Log một lần ở đây vì Render thấy *errors.Error hợp lệ là bỏ qua log.
	logs.Error(err, "chat/search: không kết nối được dịch vụ AI")
	return errors.NewCustomHttpError(http.StatusServiceUnavailable, http.StatusServiceUnavailable, aiUnavailableMessage)
}

// providerError đổi lỗi backend LLM thành lỗi HTTP đọc được thay vì 500
// "internal server error". Model không tồn tại/không được hỗ trợ (400, 404,
// 422) là lỗi phía request nên trả 400 kèm message của provider — giữ nguyên
// 500 thì người gọi API không biết mình gõ sai model hay provider đang chết.
// Hết quota/tốc độ (429) trả 429; còn lại (401 key sai, 5xx) là sự cố cấu
// hình/backend phía server → 502, không phải lỗi của client.
//
// Không phải lỗi HTTP của provider thì thử dependencyError: dịch vụ AI phía
// dưới chưa chạy → 503. Lỗi còn lại (parse, ctx...) trả nguyên để Render log
// stack và map 500 như cũ.
func providerError(err error, model string) error {
	pe, ok := eino.AsProviderHTTPError(err)
	if !ok {
		if dep := dependencyError(err); dep != nil {
			return dep
		}
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
