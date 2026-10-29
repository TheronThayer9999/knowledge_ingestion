package apis

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"knowledge_ingestion/src/controller/dtos"

	"github.com/gin-gonic/gin"
)

// bindUserProvider chạy đúng bước bind của handler — dùng chung tag `binding`
// nên test này bắt được lỗi "sửa một ô bị chặn ở tầng DTO" mà unit test
// service không thấy (service gọi thẳng, không qua bind).
func bindUserProvider(t *testing.T, body string) (*dtos.SaveUserProviderRequest, error) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/llm/my/providers/1", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	req := &dtos.SaveUserProviderRequest{}
	return req, c.ShouldBind(req)
}

// TestUserProviderFormAcceptsPartialUpdate khoá luật "bỏ trống thì giữ giá
// trị cũ": sửa CHỈ nhãn là body thiếu nhà cung cấp + key — nếu DTO đánh dấu
// required thì bind chặn và user không sửa được một ô.
func TestUserProviderFormAcceptsPartialUpdate(t *testing.T) {
	if _, err := bindUserProvider(t, `{"label":"Groq Free"}`); err != nil {
		t.Fatalf("sửa một ô không được bị chặn ở tầng binding: %v", err)
	}
	if _, err := bindUserProvider(t, `{"model":"llama-3.3-70b"}`); err != nil {
		t.Fatalf("đổi model không được bị chặn ở tầng binding: %v", err)
	}
}

// TestUserProviderFormKeepsLengthLimit: bỏ required thì vẫn phải chặn giá trị
// quá dài — tag `max` là hàng rào, không phải chi tiết thừa.
func TestUserProviderFormKeepsLengthLimit(t *testing.T) {
	long := strings.Repeat("a", 65)
	if _, err := bindUserProvider(t, `{"label":"`+long+`"}`); err == nil {
		t.Error("nhãn quá 64 ký tự phải bị chặn")
	}
	if _, err := bindUserProvider(t, `{"provider":"`+long+`"}`); err == nil {
		t.Error("slug provider quá 64 ký tự phải bị chặn")
	}
}

// TestUserProviderFormHasNoBaseURL: user KHÔNG tự gõ URL được — DTO không có
// field base_url nên body gửi lên bị bỏ qua; server tự tra định nghĩa của
// provider trong danh mục.
func TestUserProviderFormHasNoBaseURL(t *testing.T) {
	req, err := bindUserProvider(t, `{"provider":"groq","base_url":"https://evil.example/v1"}`)
	if err != nil {
		t.Fatalf("body hợp lệ: %v", err)
	}
	if req.Provider != "groq" {
		t.Errorf("provider = %q, muốn groq", req.Provider)
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "base_url") {
		t.Errorf("DTO không được mang base_url: %s", raw)
	}
}

// TestUserProviderFormRejectsWrongJSON: body hỏng thì bind phải báo lỗi —
// không lọt xuống service với form rỗng.
func TestUserProviderFormRejectsWrongJSON(t *testing.T) {
	if _, err := bindUserProvider(t, `{"label":`); err == nil {
		t.Error("JSON hỏng phải bị chặn ở tầng binding")
	}
}
