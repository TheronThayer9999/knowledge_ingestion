package dtos

import "knowledge_ingestion/src/common/errors"

type ResponseResource struct {
	Status  bool   `json:"status" example:"true"`
	Code    int    `json:"code" example:"0"`
	Message string `json:"message" example:"success"`
	Data    any    `json:"data,omitempty"`
}

func Success(data any) ResponseResource {
	return ResponseResource{
		Status:  true,
		Code:    errors.Success,
		Message: "success",
		Data:    data,
	}
}
