package apis

import (
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/domain/dtos"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

type baseController struct {
	validate *validator.Validate
}

func NewBaseController(validate *validator.Validate) *baseController {
	return &baseController{validate: validate}
}

func (b *baseController) validateRequest(request interface{}) error {
	return b.validate.Struct(request)
}

func (b *baseController) Bind(c *gin.Context, request interface{}) bool {
	if err := c.ShouldBind(request); err != nil {
		b.BadRequest(c, err.Error())
		return false
	}
	if err := b.validateRequest(request); err != nil {
		b.BadRequest(c, err.Error())
		return false
	}
	return true
}

func (b *baseController) Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, dtos.Success(data))
}

func (b *baseController) ErrorData(c *gin.Context, err *errors.Error) {
	c.JSON(err.GetHttpCode(), dtos.ResponseResource{
		Status:  false,
		Code:    err.Code,
		Message: err.Message,
	})
}

func (b *baseController) BadRequest(c *gin.Context, message string) {
	b.ErrorData(c, errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, message))
}

type renderer interface {
	Success(c *gin.Context, data interface{})
	ErrorData(c *gin.Context, err *errors.Error)
	BadRequest(c *gin.Context, message string)
}

func Render[T any](c *gin.Context, r renderer, res dtos.Result[T]) {
	if res.Err != nil {
		if e := errors.From(res.Err); e != nil {
			r.ErrorData(c, e)
			return
		}
		logs.Error(res.Err, "unhandled error")
		r.ErrorData(c, errors.ErrInternal)
		return
	}
	r.Success(c, res.Data)
}
