package errors

import (
	stderrors "errors"
	"net/http"
)

const (
	Success    = 200
	BadRequest = 400
	Internal   = 500
)

type Error struct {
	Code     int    `json:"code"`
	Message  string `json:"message"`
	HttpCode int    `json:"-"`
}

func (e *Error) Error() string {
	return e.Message
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
