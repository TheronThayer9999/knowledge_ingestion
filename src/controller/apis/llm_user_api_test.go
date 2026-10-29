package apis

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"knowledge_ingestion/src/controller/dtos"

	"github.com/gin-gonic/gin"
)

// bindUserProvider chạy đúng bước bind của handler — dùng chung tag `binding`
// nên test này bắt được lỗi "sửa một ô bị chặn ở tầng DTO" (thiếu giao thức +
// base URL) mà unit test service không thấy (service gọi thẳng, không qua
// bind).
func bindUserProvider(t *testing.T, body string) error {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/llm/my/providers/1", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	req := &dtos.SaveUserProviderRequest{}
	return c.ShouldBind(req)
}

// TestUserProviderFormAcceptsPartialUpdate khoá luật "bỏ trống thì giữ giá
// trị cũ": sửa CHỈ nhãn là body thiếu giao thức + base URL — nếu DTO đánh dấu
// required thì bind chặn và user không sửa được một ô.
func TestUserProviderFormAcceptsPartialUpdate(t *testing.T) {
	if err := bindUserProvider(t, `{"label":"Groq Free"}`); err != nil {
		t.Fatalf("sửa một ô không được bị chặn ở tầng binding: %v", err)
	}
	if err := bindUserProvider(t, `{"model":"llama-3.3-70b"}`); err != nil {
		t.Fatalf("đổi model không được bị chặn ở tầng binding: %v", err)
	}
}

// TestUserProviderFormKeepsLengthLimit: bỏ required thì vẫn phải chặn giá trị
// quá dài — tag `max` là hàng rào, không phải chi tiết thừa.
func TestUserProviderFormKeepsLengthLimit(t *testing.T) {
	long := strings.Repeat("a", 65)
	if err := bindUserProvider(t, `{"label":"`+long+`"}`); err == nil {
		t.Error("nhãn quá 64 ký tự phải bị chặn")
	}
	longURL := "http://" + strings.Repeat("b", 512) + ".example/v1"
	if err := bindUserProvider(t, `{"base_url":"`+longURL+`"}`); err == nil {
		t.Error("base URL quá 512 ký tự phải bị chặn")
	}
}

// TestUserProviderFormRejectsWrongJSON: body hỏng thì bind phải báo lỗi —
// không lọt xuống service với form rỗng.
func TestUserProviderFormRejectsWrongJSON(t *testing.T) {
	if err := bindUserProvider(t, `{"label":`); err == nil {
		t.Error("JSON hỏng phải bị chặn ở tầng binding")
	}
}
