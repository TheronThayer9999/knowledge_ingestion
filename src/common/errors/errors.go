package errors

import (
	"context"
	stderrors "errors"
	"net/http"
	"strings"
)

const (
	Success    = 0
	BadRequest = 400
	Internal   = 500
	// ClientClosed là 499 "client closed request" — quy ước của nginx, không
	// có trong RFC nên net/http cũng không có hằng. Chỉ dùng cho lượt client
	// tự ngắt giữa đường: server không hỏng gì, nhưng access log cần con số
	// khác 200/500 để phân biệt.
	ClientClosed = 499
)

type Error struct {
	// Code là mã nghiệp vụ đi kèm trong body (0→OK). Service dựng lỗi bằng
	// NewCustomHttpError nên Code thường trùng HttpCode (400/401/403/404...);
	// HttpCode mới là thứ status line dùng. Đừng nhét mã riêng cho từng
	// transport vào đây.
	Code    int    `json:"code"`
	Message string `json:"message"`
	// HttpCode chỉ HTTP dùng (status line) — không serialize ra body.
	HttpCode int `json:"-"`
}

func (e *Error) Error() string {
	return e.Message
}

// GetCode trả mã lỗi nghiệp vụ cho adapter map sang
// mã trạng thái của transport mình.
func (e *Error) GetCode() int {
	return e.Code
}

func (e *Error) GetHttpCode() int {
	return e.HttpCode
}

func NewCustomHttpError(httpCode int, code int, message string) *Error {
	return &Error{Code: code, Message: message, HttpCode: httpCode}
}

func From(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if stderrors.As(err, &e) {
		return e
	}
	return nil
}

var ErrBadRequest = NewCustomHttpError(http.StatusBadRequest, BadRequest, "bad request")
var ErrInternal = NewCustomHttpError(http.StatusInternalServerError, Internal, "internal server error")

// contextMarkers nhận diện lỗi ctx qua TEXT: adk của eino bọc lỗi model/tool
// thành NodeRunError nên chuỗi Unwrap đứt, errors.Is không xuyên qua được
// (cùng lý do với depDownMarkers bên usecase).
var (
	cancelMarkers   = []string{"context canceled"}
	deadlineMarkers = []string{"context deadline exceeded"}
)

// ContextError đổi lỗi do ctx bị hủy/quá hạn thành lỗi HTTP đọc được, nil khi
// không liên quan (caller giữ đường 500 + stack). Client ngắt (nút Dừng, đóng
// tab, curl --max-time) trả 499; provider không kịp trả lời trong timeout_sec
// trả 504 — trước đây cả hai đều thành 500 "internal server error", client
// không biết là hết giờ hay server hỏng. Message không chứa URL/cổng
// (dial tcp, localhost:20128) của hạ tầng nội bộ.
func ContextError(err error) *Error {
	if err == nil {
		return nil
	}
	if stderrors.Is(err, context.Canceled) || hasMarker(err.Error(), cancelMarkers) {
		return NewCustomHttpError(ClientClosed, ClientClosed,
			"client đã ngắt yêu cầu trước khi xử lý xong")
	}
	if stderrors.Is(err, context.DeadlineExceeded) || hasMarker(err.Error(), deadlineMarkers) {
		return NewCustomHttpError(http.StatusGatewayTimeout, http.StatusGatewayTimeout,
			"hết thời gian chờ: dịch vụ phía sau không phản hồi kịp — thử lại hoặc thu hẹp phạm vi yêu cầu")
	}
	return nil
}

func hasMarker(msg string, markers []string) bool {
	lower := strings.ToLower(msg)
	for _, m := range markers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}
