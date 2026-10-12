package eino

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// Ngưỡng kích tóm tắt lịch sử hội thoại — docs Eino khuyên đặt 80-90% context
// window của model, nhưng free model không công bố window nên đặt bảo thủ
// (16k token ≈ 64k ký tự) để tóm sớm thay vì để tràn context rồi 500.
// ContextMessages=60: ~30 lượt hội thoại; vượt thì tóm trước lần gọi model
// đầu tiên của lượt hiện tại.
const (
	summarizeTriggerTokens   = 16000
	summarizeTriggerMessages = 60
	summarizeMaxRetries      = 3
)

// newSummarizeMiddleware dựng summarization middleware (docs:
// cloudwego.io/docs/eino/.../middleware_summarization) — tự gọi LLM tóm lịch
// sử khi vượt ngưỡng, thay cách cắt cứng maxHistoryTurns. Retry 3 lần vì
// summary lỗi sẽ làm hỏng cả lượt chat (BeforeModelRewriteState trả lỗi thì
// model call fail). Model tóm = client của provider (dùng model mặc định của
// provider — request có ép model riêng thì phần tóm vẫn chạy model mặc định,
// chấp nhận được vì tóm lịch sử không cần model mạnh nhất).
func newSummarizeMiddleware(model einomodel.ToolCallingChatModel) (adk.ChatModelAgentMiddleware, error) {
	maxRetries := summarizeMaxRetries
	return summarization.New(context.Background(), &summarization.Config{
		Model: model,
		Trigger: &summarization.TriggerCondition{
			ContextTokens:   summarizeTriggerTokens,
			ContextMessages: summarizeTriggerMessages,
		},
		Retry: &summarization.RetryConfig{MaxRetries: &maxRetries},
		// Custom Finalize: mặc định framework tóm TOÀN BỘ context (kể cả tool
		// result của vòng lặp hiện tại) → mất căn cứ [^n] giữa chừng. Giữ đuôi
		// từ user message cuối (câu hỏi + mọi tool result của lượt này) sau
		// phần summary → chỉ lịch sử lượt cũ bị nén.
		Finalize: keepRunTailFinalize,
	})
}

// keepRunTailFinalize = DefaultFinalize (tóm + system message) nhưng cộng lại
// phần đuôi sau user message cuối của bản gốc — vòng tool đang chạy không bị
// tóm nên model vẫn thấy kết quả đọc/search vừa lấy để chốt câu trả lời.
func keepRunTailFinalize(ctx context.Context, orig []*schema.Message, summary *schema.Message) ([]*schema.Message, error) {
	// Model free đôi khi trả summary rỗng (content "") → DefaultFinalize báo
	// lỗi "summary content is empty" và làm hỏng cả lượt chat. Thà không tóm
	// (context giữ nguyên, chỉ tốn token) còn hơn fail câu trả lời.
	if messageText(summary) == "" {
		return orig, nil
	}
	out, err := summarization.DefaultFinalize(ctx, orig, summary)
	if err != nil {
		return nil, err
	}
	for i := len(orig) - 1; i >= 0; i-- {
		if orig[i].Role != schema.User {
			continue
		}
		if i+1 < len(orig) {
			out = append(out, orig[i+1:]...)
		}
		break
	}
	return out, nil
}

// messageText lấy text của message do model sinh (assistant) — Content tường
// minh, hoặc fallback sang multi-part array (model free đôi khi trả dạng
// parts khiến .Content rỗng dù model đã trả lời). Mirror logic
// getAssistantTextContent nội bộ của summarization package (không export).
func messageText(m *schema.Message) string {
	if m == nil || m.Role != schema.Assistant {
		return ""
	}
	var parts []string
	for _, p := range m.AssistantGenMultiContent {
		if p.Type == schema.ChatMessagePartTypeText && p.Text != "" {
			parts = append(parts, p.Text)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	return m.Content
}
