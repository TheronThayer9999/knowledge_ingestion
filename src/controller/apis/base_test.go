package apis

import (
	"encoding/json"
	"fmt"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/domain/dtos"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

func newRenderer() *baseController {
	gin.SetMode(gin.TestMode)
	return NewBaseController(validator.New())
}

func doRender(t *testing.T, res dtos.Result[string]) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	Render(c, newRenderer(), res)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) dtos.ResponseResource {
	t.Helper()
	var body dtos.ResponseResource
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid body: %v", err)
	}
	return body
}

func TestRenderSuccess(t *testing.T) {
	w := doRender(t, dtos.Ok("hello"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := decodeBody(t, w)
	if !body.Status || body.Code != errors.Success {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestRenderCustomErrorKeepsCode(t *testing.T) {
	w := doRender(t, dtos.Fail[string](errors.NewCustomHttpError(http.StatusNotFound, 404, "not found")))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	body := decodeBody(t, w)
	if body.Status || body.Code != 404 {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestRenderUnknownErrorBecomesInternal(t *testing.T) {
	w := doRender(t, dtos.Fail[string](fmt.Errorf("db exploded")))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	body := decodeBody(t, w)
	if body.Status || body.Code != errors.Internal {
		t.Fatalf("unexpected body: %+v", body)
	}
	if body.Message != "internal server error" {
		t.Fatalf("internal details leaked: %q", body.Message)
	}
}
