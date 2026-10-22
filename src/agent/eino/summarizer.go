package eino

import (
	"context"
	"fmt"
	"strings"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"knowledge_ingestion/src/domain"
)

// SectionSummarizer triển khai domain.ISummarizer trên ChatModel Eino —
// compile phase 2 (P2.4 phase 1b) gọi cho từng mục tài liệu. Cùng pattern
// question.LLMClassifier: 1 system prompt load từ file lúc startup, hỏi 1
// câu system+user, temperature 0 (tóm tắt thì không bịa, không cần "sáng tạo").
type SectionSummarizer struct {
	model     einomodel.BaseChatModel
	modelName string
	system    string
}

var _ domain.ISummarizer = (*SectionSummarizer)(nil)

// NewSectionSummarizer ghép client + tên model (đưa vào hash cache) + system
// prompt. Model dựng từ llms.default_provider ở loader worker — nơi này chỉ
// nhận gì dùng nấy.
func NewSectionSummarizer(model einomodel.BaseChatModel, modelName, system string) *SectionSummarizer {
	return &SectionSummarizer{model: model, modelName: modelName, system: strings.TrimSpace(system)}
}

func (s *SectionSummarizer) ModelID() string { return s.modelName }

// Summarize gọi LLM tóm tắt 1 mục — user message ghép tên mục + nội dung
// để model bám đúng phạm vi Điều đang tóm. Model chưa cấu hình hoặc trả lời
// rỗng thì lỗi để service đánh dấu bài failed/backoff, KHÔNG im lặng ghi
// summary trống (compile xong mà rỗng thì không phân biệt được với bài chưa
// compile).
func (s *SectionSummarizer) Summarize(ctx context.Context, sectionName, text string) (string, error) {
	if s == nil || s.model == nil || s.system == "" {
		return "", fmt.Errorf("eino summarizer: chưa có chat model/system prompt (kiểm tra llms.default_provider + prompts/compile_section.md)")
	}
	user := "Mục: " + sectionName + "\n\nNội dung:\n" + text
	out, err := s.model.Generate(ctx,
		[]*schema.Message{
			schema.SystemMessage(s.system),
			schema.UserMessage(user),
		},
		einomodel.WithTemperature(0))
	if err != nil {
		return "", fmt.Errorf("eino summarizer: %w", err)
	}
	if out == nil || strings.TrimSpace(out.Content) == "" {
		return "", fmt.Errorf("eino summarizer: model trả lời rỗng cho mục %q", sectionName)
	}
	return strings.TrimSpace(out.Content), nil
}
