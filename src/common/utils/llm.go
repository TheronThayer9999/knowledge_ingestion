package utils

import "context"

// llmSiteUnknown nhãn khi không ai gắn phase — thấy nhãn này trong log nghĩa
// là có chỗ gọi LLM mới chưa gắn site, cần bổ sung chứ không phải "call ma".
const llmSiteUnknown = "không rõ"

type llmSiteKey struct{}

// WithLLMSite gắn nhãn phase (constants.LLMSite*) vào ctx. Nhãn không nằm
// trong chữ ký hàm nào của thư viện LLM: nó theo ctx xuống tận transport HTTP,
// nên agent loop, middleware tóm tắt lịch sử hay classifier đều được gắn mà
// không phải sửa interface của Eino.
func WithLLMSite(ctx context.Context, site string) context.Context {
	if ctx == nil || site == "" {
		return ctx
	}
	return context.WithValue(ctx, llmSiteKey{}, site)
}

// LLMSiteFromCtx đọc nhãn phase do tầng trên gắn — ctx không có thì trả
// "không rõ" để dòng log vẫn đủ trường, không bị rỗng khó grep.
func LLMSiteFromCtx(ctx context.Context) string {
	if ctx == nil {
		return llmSiteUnknown
	}
	if v, ok := ctx.Value(llmSiteKey{}).(string); ok && v != "" {
		return v
	}
	return llmSiteUnknown
}
