package usecase

import (
	stderrors "errors"
	"fmt"
	"net/http"
	"strings"
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

// Lỗi không phải HTTP của provider phải giữ nguyên để Render log stack và trả
// 500 như cũ — không được nuốt thành 400/502.
func TestProviderErrorLeavesOtherErrors(t *testing.T) {
	orig := stderrors.New("dial tcp 127.0.0.1:11434: connect: connection refused")
	if got := providerError(orig, "m"); got != orig {
		t.Errorf("muốn giữ nguyên lỗi gốc, nhận %v", got)
	}
}
