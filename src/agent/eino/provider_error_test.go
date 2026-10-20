package eino

import (
	"errors"
	"fmt"
	"testing"

	openaiprovider "github.com/cloudwego/eino-ext/components/model/openai"
)

func TestAsProviderHTTPErrorTyped(t *testing.T) {
	base := &openaiprovider.APIError{
		HTTPStatusCode: 404,
		HTTPStatus:     "404 Not Found",
		Message:        "No endpoints found for apodex/apodex-1.1-mini:free.",
	}
	// eino wrap bằng %w thì errors.As phải xuyên qua được.
	cases := map[string]error{
		"trực tiếp":  base,
		"wrap 1 lớp": fmt.Errorf("eino agent: %w", base),
	}
	for name, err := range cases {
		pe, ok := AsProviderHTTPError(err)
		if !ok {
			t.Fatalf("%s: muốn nhận lỗi provider, nhận false", name)
		}
		if pe.Status != 404 {
			t.Errorf("%s: status = %d, muốn 404", name, pe.Status)
		}
		if pe.Message != "No endpoints found for apodex/apodex-1.1-mini:free." {
			t.Errorf("%s: message = %q", name, pe.Message)
		}
	}
}

// TestAsProviderHTTPErrorFromText khoá đường dự phòng: adk bọc lỗi node thành
// NodeRunError nên chuỗi Unwrap đứt, chỉ còn text — vẫn phải rút được status và
// message, KHÔNG kéo theo "node path: ..." phía sau.
func TestAsProviderHTTPErrorFromText(t *testing.T) {
	err := errors.New("eino agent: [NodeRunError] error, status code: 429, status: 429 Too Many Requests, " +
		"message: Rate limit exceeded.\n------------------------\nnode path: [node_1, ChatModel]")
	pe, ok := AsProviderHTTPError(err)
	if !ok {
		t.Fatal("muốn nhận lỗi provider từ text, nhận false")
	}
	if pe.Status != 429 {
		t.Errorf("status = %d, muốn 429", pe.Status)
	}
	if pe.Message != "Rate limit exceeded." {
		t.Errorf("message = %q, muốn %q", pe.Message, "Rate limit exceeded.")
	}
}

func TestAsProviderHTTPErrorIgnoresOtherErrors(t *testing.T) {
	if _, ok := AsProviderHTTPError(nil); ok {
		t.Error("nil không phải lỗi provider")
	}
	if _, ok := AsProviderHTTPError(errors.New("dial tcp: connection refused")); ok {
		t.Error("lỗi mạng không phải lỗi HTTP của provider")
	}
	if _, ok := AsProviderHTTPError(&openaiprovider.APIError{Message: "boom"}); ok {
		t.Error("APIError không có status code thì không đủ căn cứ map")
	}
}
