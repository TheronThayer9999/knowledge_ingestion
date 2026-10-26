package usecase

import (
	"context"
	stderrors "errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"

	"knowledge_ingestion/src/common/errors"
)

// upstreamErr dựng lỗi giống eino trả về (text đã mất chuỗi Unwrap) để test
// mapping mà không cần gọi provider thật.
func upstreamErr(status int, msg string) error {
	return fmt.Errorf("eino agent: [NodeRunError] error, status code: %d, status: %d X, message: %s\n"+
		"------------------------\nnode path: [node_1, ChatModel]", status, status, msg)
}

func TestProviderErrorMapsModelNotFoundTo400(t *testing.T) {
	err := providerError(upstreamErr(404, "No endpoints found for apodex/apodex-1.1-mini:free."), "apodex/apodex-1.1-mini:free")

	e := errors.From(err)
	if e == nil {
		t.Fatalf("muốn *errors.Error, nhận %T", err)
	}
	if e.GetHttpCode() != http.StatusBadRequest {
		t.Errorf("HttpCode = %d, muốn 400", e.GetHttpCode())
	}
	for _, want := range []string{"apodex/apodex-1.1-mini:free", "No endpoints found"} {
		if !strings.Contains(e.Message, want) {
			t.Errorf("message %q thiếu %q", e.Message, want)
		}
	}
}

func TestProviderErrorMapsQuotaTo429(t *testing.T) {
	e := errors.From(providerError(upstreamErr(429, "Rate limit exceeded."), "nvidia/nemotron-3.5-super-vl:free"))
	if e == nil || e.GetHttpCode() != http.StatusTooManyRequests {
		t.Fatalf("muốn 429, nhận %+v", e)
	}
}

func TestProviderErrorMapsUpstreamFailureTo502(t *testing.T) {
	for _, status := range []int{401, 500, 503} {
		e := errors.From(providerError(upstreamErr(status, "upstream down"), "m"))
		if e == nil || e.GetHttpCode() != http.StatusBadGateway {
			t.Errorf("status %d: muốn 502, nhận %+v", status, e)
		}
	}
}

// Lỗi không thuộc hai nhóm trên (parse, logic...) phải giữ nguyên để Render log
// stack và trả 500 như cũ — không được nuốt thành 400/502/503.
func TestProviderErrorLeavesOtherErrors(t *testing.T) {
	orig := stderrors.New("json: cannot unmarshal object into Go value of type []string")
	if got := providerError(orig, "m"); got != orig {
		t.Errorf("muốn giữ nguyên lỗi gốc, nhận %v", got)
	}
}

// Client ngắt giữa đường (nút Dừng, đóng tab, curl --max-time): 499 — không
// phải 500 "server hỏng" và cũng không phải 503 "hạ tầng chết", vì dịch vụ
// vẫn đang sống.
func TestProviderErrorMapsClientCancelTo499(t *testing.T) {
	real := stderrors.New(`eino agent: [NodeRunError] failed to create chat completion: ` +
		`Post "http://localhost:20128/v1/chat/completions": context canceled\n` +
		"------------------------\nnode path: [node_1, ChatModel]")

	e := errors.From(providerError(real, "kc/nvidia/nemotron-3.5-super-vl:free"))
	if e == nil || e.GetHttpCode() != errors.ClientClosed {
		t.Fatalf("muốn 499, nhận %+v", e)
	}
}

// Provider không trả lời trong timeout_sec (http.Client.Timeout): 504 để người
// gọi biết là hết giờ chờ, không phải lỗi nội bộ.
func TestProviderErrorMapsProviderTimeoutTo504(t *testing.T) {
	real := stderrors.New(`eino agent: [NodeRunError] failed to create chat completion: ` +
		`Post "http://localhost:20128/v1/chat/completions": context deadline exceeded ` +
		`(Client.Timeout exceeded while awaiting headers)`)

	e := errors.From(providerError(real, "kc/nvidia/nemotron-3.5-super-vl:free"))
	if e == nil || e.GetHttpCode() != http.StatusGatewayTimeout {
		t.Fatalf("muốn 504, nhận %+v", e)
	}
}

// Chuỗi thật từ log khi Ollama local chưa chạy: adk bọc lỗi tool thành
// NodeRunError nên Unwrap đứt, chỉ còn text — vẫn phải ra 503, và message
// không được làm lộ URL/cổng/`dial tcp` của hạ tầng nội bộ.
func TestProviderErrorMapsDependencyDownTo503(t *testing.T) {
	real := stderrors.New("eino agent: [NodeRunError] failed to invoke tool[name:knowledge_search " +
		"id:call-8123]: [LocalFunc] failed to invoke tool, toolName=knowledge_search, " +
		"err=hybrid search: embed query: ollama: call embed api: Post \"http://localhost:11434/api/embed\": " +
		"dial tcp [::1]:11434: connectex: No connection could be made because the target machine " +
		"actively refused it.\n------------------------\nnode path: [node_1, ToolNode]")

	e := errors.From(providerError(real, "gemma4:31b"))
	if e == nil || e.GetHttpCode() != http.StatusServiceUnavailable {
		t.Fatalf("muốn 503, nhận %+v", e)
	}
	for _, leak := range []string{"11434", "localhost", "dial tcp", "NodeRunError"} {
		if strings.Contains(e.Message, leak) {
			t.Errorf("message %q làm lộ %q", e.Message, leak)
		}
	}
}

// Đường searchService gọi thẳng pipeline: chuỗi Unwrap còn nguyên nên bắt
// bằng *net.OpError, khỏi cần đoán text.
func TestDependencyErrorFromTypedNetError(t *testing.T) {
	wrapped := fmt.Errorf("hybrid search: embed query: %w",
		&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED})
	if dependencyError(wrapped) == nil {
		t.Fatal("muốn 503 từ *net.OpError, nhận nil")
	}
}

// Client bấm Dừng / tab đóng làm ctx hủy — dịch vụ vẫn sống, báo hạ tầng chết
// là sai và còn khiến người dùng đi sửa nhầm thứ.
func TestDependencyErrorIgnoresClientCancel(t *testing.T) {
	cancelled := fmt.Errorf("ollama: call embed api: %w", context.Canceled)
	if got := dependencyError(cancelled); got != nil {
		t.Errorf("muốn nil cho lỗi cancel, nhận %v", got)
	}
	if got := dependencyError(stderrors.New("agent hết vòng lặp")); got != nil {
		t.Errorf("muốn nil cho lỗi không liên quan mạng, nhận %v", got)
	}
}
