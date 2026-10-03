package apis

import (
	"knowledge_ingestion/src/common/errors"
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
	c.JSON(http.StatusOK, dtos.ResponseResource{
		Status:  true,
		Code:    errors.Success,
		Message: "success",
		Data:    data,
	})
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

func Render[T any](c *gin.Context, b *baseController, res dtos.Result[T]) {
	if res.Err != nil {
		if e := errors.From(res.Err); e != nil {
			b.ErrorData(c, e)
			return
		}
		b.BadRequest(c, res.Err.Error())
		return
	}
	b.Success(c, res.Data)
}
