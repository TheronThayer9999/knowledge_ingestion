package dtos

import "knowledge_ingestion/src/common/errors"

type ResponseResource struct {
	Status  bool        `json:"status" example:"true"`
	Code    int         `json:"code" example:"0"`
	Message string      `json:"message" example:"success"`
	Data    interface{} `json:"data,omitempty"`
}

func Success(data interface{}) ResponseResource {
	return ResponseResource{
		Status:  true,
		Code:    errors.Success,
		Message: "success",
		Data:    data,
	}
}
