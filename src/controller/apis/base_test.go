package apis

import (
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/controller/dtos"

	"github.com/gin-gonic/gin"
)

// TestResponseShape luôn giữ 4 field trong mọi response — cùng một tập key
// cho thành công, lỗi nghiệp vụ, lỗi 500 lẫn 401 của middleware. Trước đây
// Data có omitempty nên response lỗi rơi còn 3 field, client phải dò key.
// Test này là hợp đồng: muốn đổi envelope thì sửa cả test này.
func TestResponseShape(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name     string
		res      dtos.Result[string]
		wantCode int
		wantHTTP int
	}{
		{"thành công", dtos.Ok("payload"), errors.Success, http.StatusOK},
		{"lỗi nghiệp vụ", dtos.Fail[string](
			errors.NewCustomHttpError(http.StatusNotFound, http.StatusNotFound, "bài viết không tồn tại")),
			http.StatusNotFound, http.StatusNotFound},
		{"lỗi không phải *errors.Error", dtos.Fail[string](stderrors.New("bùng nổ ở tầng repo")),
			errors.Internal, http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

			Render(c, &baseController{}, tc.res)

			if w.Code != tc.wantHTTP {
				t.Errorf("status line = %d, want %d", w.Code, tc.wantHTTP)
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("body không phải JSON object: %v\n%s", err, w.Body.String())
			}
			got := make([]string, 0, len(body))
			for k := range body {
				got = append(got, k)
			}
			sort.Strings(got)
			want := []string{"code", "data", "message", "status"}
			if len(got) != len(want) {
				t.Fatalf("số field = %d (%v), want %d (%v)", len(got), got, len(want), want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("field[%d] = %q, want %q (đủ bộ: %v)", i, got[i], want[i], got)
				}
			}

			var env dtos.ResponseResource
			if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
				t.Fatalf("unmarshal envelope: %v", err)
			}
			if env.Code != tc.wantCode {
				t.Errorf("code = %d, want %d", env.Code, tc.wantCode)
			}
			if env.Status != (tc.res.Err == nil) {
				t.Errorf("status = %v, want %v", env.Status, tc.res.Err == nil)
			}
		})
	}
}

// TestErrorHidesInternalMessage chặn rò chi tiết lỗi nội bộ ra ngoài — lỗi
// không phải *errors.Error phải về 500 với message chung, không phải 400 và
// không mang text gốc (có thể chứa SQL, đường dẫn file, connection string).
func TestErrorHidesInternalMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	leak := "pq: password authentication failed for user \"theron\""
	Render(c, &baseController{}, dtos.Fail[string](stderrors.New(leak)))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status line = %d, want 500 (lỗi lạ không được trả 400)", w.Code)
	}
	if got := w.Body.String(); strings.Contains(got, leak) {
		t.Errorf("body lộ chi tiết lỗi nội bộ:\n%s", got)
	}
}
