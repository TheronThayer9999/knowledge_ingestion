package utils

import (
	"context"
	"testing"
)

// TestLLMSiteContext khoá hợp đồng nhãn phase: ctx chưa gắn thì "không rõ"
// (để log lộ chỗ gọi thiếu nhãn), gắn rồi thì đọc lại đúng, ctx nil không panic.
func TestLLMSiteContext(t *testing.T) {
	if got := LLMSiteFromCtx(context.Background()); got != llmSiteUnknown {
		t.Errorf("ctx chưa gắn = %q, muốn %q", got, llmSiteUnknown)
	}
	//nolint:staticcheck // chủ ý: kiểm nhánh ctx nil của hàm đọc nhãn
	if got := LLMSiteFromCtx(nil); got != llmSiteUnknown {
		t.Errorf("ctx nil = %q, muốn %q", got, llmSiteUnknown)
	}
	ctx := WithLLMSite(context.Background(), "compile")
	if got := LLMSiteFromCtx(ctx); got != "compile" {
		t.Errorf("sau khi gắn = %q, muốn compile", got)
	}
	// Gắn site rỗng là "không đổi gì" — không tạo nhãn rỗng khó grep.
	if got := LLMSiteFromCtx(WithLLMSite(ctx, "")); got != "compile" {
		t.Errorf("site rỗng phải giữ nhãn cũ, nhận %q", got)
	}
}
