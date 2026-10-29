package eino

import (
	"context"

	"github.com/cloudwego/eino/callbacks"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"knowledge_ingestion/src/common/llmusage"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/domain"
)

// usageHandler bắt mọi lượt gọi chat model để đếm token — nền của audit
// "tài khoản này dùng bao nhiêu LLM".
//
// Đăng ký GLOBAL (callbacks.AppendGlobalHandlers) nên bắt được cả lượt gọi
// TRONG vòng agent (adk gọi model nhiều lần cho 1 câu hỏi) lẫn lượt gọi
// đứng đơn (compile tóm tắt mục, classifier dán nhãn) — không phải sửa chữ
// ký hàm nào, không phải canh "chỗ nào gọi LLM".
//
// Collector đi theo ctx: lượt nào không gắn collector thì handler im lặng —
// công cụ nội bộ gọi model cũng không bị tính nhầm.
type usageHandler struct{}

var _ callbacks.Handler = (*usageHandler)(nil)

// init đăng ký handler 1 lần cho cả process. AppendGlobalHandlers yêu cầu
// gọi lúc khởi tạo chương trình (không an toàn khi chạy đồng thời) — init()
// là đúng chỗ đó, và cả API lẫn worker đều import package này nên không cần
// nhớ gọi ở từng main.
func init() {
	callbacks.AppendGlobalHandlers(&usageHandler{})
}

// OnStart đếm 1 lượt gọi model. Chỉ nhận input kiểu model.CallbackInput
// (hoặc []*schema.Message khi graph tiêm vào) — input của tool, lambda hay
// node khác bị bỏ qua.
func (h *usageHandler) OnStart(ctx context.Context, _ *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
	if einomodel.ConvCallbackInput(input) == nil {
		return ctx
	}
	if c, ok := llmusage.FromCtx(ctx); ok {
		c.Add(utils.LLMSiteFromCtx(ctx), domain.LLMUsage{Calls: 1})
	}
	return ctx
}

// OnEnd cộng token provider đã trả. Token lấy từ TokenUsage của callback
// output; có callback chỉ trả message (graph tiêm output) thì đọc
// ResponseMeta.Usage của message.
func (h *usageHandler) OnEnd(ctx context.Context, _ *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
	out := einomodel.ConvCallbackOutput(output)
	if out == nil {
		return ctx
	}
	u := tokenUsageOf(out)
	if u.IsZero() {
		return ctx
	}
	if c, ok := llmusage.FromCtx(ctx); ok {
		c.Add(utils.LLMSiteFromCtx(ctx), u)
	}
	return ctx
}

// OnError đếm lượt gọi hỏng — lượt hỏng vẫn có thể đã tốn token đầu vào nên
// tách khỏi calls để quản lý thấy "hỏng bao nhiêu".
func (h *usageHandler) OnError(ctx context.Context, _ *callbacks.RunInfo, _ error) context.Context {
	if c, ok := llmusage.FromCtx(ctx); ok {
		c.Add(utils.LLMSiteFromCtx(ctx), domain.LLMUsage{Errors: 1})
	}
	return ctx
}

// OnStartWithStreamInput không xử lý — luồng stream không dùng ở đây, nhưng
// interface Handler bắt buộc implement đủ 5 hàm.
func (h *usageHandler) OnStartWithStreamInput(ctx context.Context, _ *callbacks.RunInfo,
	_ *schema.StreamReader[callbacks.CallbackInput]) context.Context {
	return ctx
}

// OnEndWithStreamOutput không xử lý — xem OnStartWithStreamInput.
func (h *usageHandler) OnEndWithStreamOutput(ctx context.Context, _ *callbacks.RunInfo,
	_ *schema.StreamReader[callbacks.CallbackOutput]) context.Context {
	return ctx
}

// tokenUsageOf rút số liệu token từ output của callback — ưu tiên TokenUsage
// (chat model trả trực tiếp), thiếu thì đọc ResponseMeta.Usage của message.
func tokenUsageOf(out *einomodel.CallbackOutput) domain.LLMUsage {
	if out == nil {
		return domain.LLMUsage{}
	}
	if tu := out.TokenUsage; tu != nil {
		return domain.LLMUsage{
			PromptTokens:     tu.PromptTokens,
			CompletionTokens: tu.CompletionTokens,
			TotalTokens:      tu.TotalTokens,
		}
	}
	if out.Message != nil && out.Message.ResponseMeta != nil && out.Message.ResponseMeta.Usage != nil {
		su := out.Message.ResponseMeta.Usage
		return domain.LLMUsage{
			PromptTokens:     su.PromptTokens,
			CompletionTokens: su.CompletionTokens,
			TotalTokens:      su.TotalTokens,
		}
	}
	return domain.LLMUsage{}
}
