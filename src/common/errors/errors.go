package errors

import (
	stderrors "errors"
	"net/http"
)

const (
	Success    = 0
	BadRequest = 400
	Internal   = 500
)

type Error struct {
	// Code là mã lỗi trung lập transport — HTTP Render đọc HttpCode cho
	// status line, gRPC map Code sang grpc codes qua grpcStatus (0→OK,
	// 400→InvalidArgument, 404→NotFound, còn lại→Internal). Đừng nhét mã
	// riêng cho từng transport vào đây.
	Code    int    `json:"code"`
	Message string `json:"message"`
	// HttpCode chỉ HTTP dùng (status line) — không serialize ra body, gRPC
	// không đọc field này.
	HttpCode int `json:"-"`
}

func (e *Error) Error() string {
	return e.Message
}

// GetCode trả mã lỗi trung lập transport cho adapter (HTTP/gRPC) map sang
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
