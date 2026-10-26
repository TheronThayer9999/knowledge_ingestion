package errors

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestContextErrorMapping khoá hợp đồng: ctx chết không được rơi vào 500.
// "ctx chết" có hai kiểu — client ngắt (499) và quá hạn chờ (504) — và mỗi
// kiểu phải bắt được ở cả dạng còn Unwrap lẫn dạng chỉ còn text (eino bọc
// thành NodeRunError làm đứt chuỗi Unwrap).
func TestContextErrorMapping(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		want  int
		nilOK bool
	}{
		{"cancel còn unwrap", fmt.Errorf("agent.Run: %w", context.Canceled), ClientClosed, false},
		{"cancel chỉ còn text (kiểu eino)", stderrors.New(`eino agent: [NodeRunError] failed to create chat completion: ` +
			`Post "http://localhost:20128/v1/chat/completions": context canceled` + "\n------------------------\n" +
			"node path: [node_1, ChatModel]"), ClientClosed, false},
		{"deadline còn unwrap", fmt.Errorf("http client: %w", context.DeadlineExceeded), http.StatusGatewayTimeout, false},
		{"deadline chỉ còn text (http.Client.Timeout)", stderrors.New(`eino agent: [NodeRunError] failed to create chat completion: ` +
			`Post "http://localhost:20128/v1/chat/completions": context deadline exceeded ` +
			`(Client.Timeout exceeded while awaiting headers)`), http.StatusGatewayTimeout, false},
		{"lỗi thật giữ đường 500", stderrors.New("json: cannot unmarshal number into Go value of type string"), 0, true},
		{"nil", nil, 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ContextError(tc.err)
			if tc.nilOK {
				if got != nil {
					t.Fatalf("muốn nil, nhận %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("muốn lỗi HTTP %d, nhận nil", tc.want)
			}
			if got.GetHttpCode() != tc.want {
				t.Errorf("HttpCode = %d, muốn %d", got.GetHttpCode(), tc.want)
			}
			if got.Code != tc.want {
				t.Errorf("Code = %d, muốn %d", got.Code, tc.want)
			}
			// Message đi thẳng ra client: không được lộ URL/cổng nội bộ.
			for _, leak := range []string{"localhost", "20128", "dial tcp", "NodeRunError"} {
				if strings.Contains(got.Message, leak) {
					t.Errorf("message %q làm lộ %q", got.Message, leak)
				}
			}
		})
	}
}
