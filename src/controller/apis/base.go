package apis

import (
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
	b.ResponseWithCode(c, http.StatusOK, "success", data)
}

func (b *baseController) BadRequest(c *gin.Context, message string) {
	b.ResponseWithCode(c, http.StatusBadRequest, message, nil)
}

func (b *baseController) ResponseWithCode(c *gin.Context, code int, message string, data interface{}) {
	c.JSON(code, dtos.ResponseResource{
		Code:    code,
		Message: message,
		Data:    data,
	})
}
