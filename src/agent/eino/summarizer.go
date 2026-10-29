package eino

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"knowledge_ingestion/src/domain"
)

// SectionSummarizer triển khai domain.ISummarizer trên ChatModel Eino —
// compile phase 2 (P2.4 phase 1b) gọi cho từng LÔ mục tài liệu. Cùng pattern
// question.LLMClassifier: system prompt load từ file lúc startup, hỏi 1
// câu system+user, temperature 0 (tóm tắt thì không bịa, không cần "sáng tạo").
//
// 2 prompt: 1 mục (compile_section.md) và N mục (compile_sections.md). Lô là
// đường chính — bài 500 trang có hàng nghìn mục, gọi 1 mục/lượt thì tốn
// hàng nghìn request LLM cho 1 tài liệu.
type SectionSummarizer struct {
	model     einomodel.BaseChatModel
	modelName string
	provider  string
	system    string
	// batchSystem là prompt cho lô N mục — model trả N dòng, mỗi dòng mở
	// đầu bằng nhãn [S<i>] để tách được từng tóm tắt.
	batchSystem string
}

var _ domain.ISummarizer = (*SectionSummarizer)(nil)

// NewSectionSummarizer ghép client + tên model + slug provider (đưa vào bảng
// audit usage) + 2 system prompt. Model dựng từ llms.default_provider ở
// loader worker — nơi này chỉ nhận gì dùng nấy.
func NewSectionSummarizer(model einomodel.BaseChatModel, modelName, provider, system, batchSystem string) *SectionSummarizer {
	return &SectionSummarizer{
		model:       model,
		modelName:   modelName,
		provider:    provider,
		system:      strings.TrimSpace(system),
		batchSystem: strings.TrimSpace(batchSystem),
	}
}

func (s *SectionSummarizer) ModelID() string    { return s.modelName }
func (s *SectionSummarizer) ProviderID() string { return s.provider }

// SummarizeBatch tóm tắt N mục trong 1 lượt gọi LLM — trả N tóm tắt cùng
// thứ tự với items.
//
// Lô chỉ còn 1 mục thì dùng prompt 1 mục (không cần nhãn [S<i>]). Model
// không theo khuôn đầu ra thì gọi lại từng mục — đắt hơn nhưng vẫn ra đủ
// tóm tắt, không mất dữ liệu.
func (s *SectionSummarizer) SummarizeBatch(ctx context.Context, items []domain.SectionContent) ([]string, error) {
	if s == nil || s.model == nil || s.system == "" {
		return nil, fmt.Errorf("eino summarizer: chưa có chat model/system prompt (kiểm tra llms.default_provider + prompts/compile_section.md)")
	}
	if len(items) == 0 {
		return []string{}, nil
	}
	if len(items) == 1 {
		one, err := s.summarizeOne(ctx, items[0])
		if err != nil {
			return nil, err
		}
		return []string{one}, nil
	}
	if s.batchSystem == "" {
		return nil, fmt.Errorf("eino summarizer: thiếu prompt cho lô mục (prompts/compile_sections.md)")
	}
	out, err := s.generate(ctx, s.batchSystem, batchUserMessage(items))
	if err != nil {
		return nil, err
	}
	if sums := parseBatchSummaries(out, len(items)); sums != nil {
		return sums, nil
	}
	return s.summarizeEach(ctx, items)
}

// summarizeOne tóm tắt 1 mục bằng prompt 1 mục — dùng cho lô cuối chỉ còn
// 1 mục và là đường dự phòng khi model không theo khuôn đầu ra của lô.
func (s *SectionSummarizer) summarizeOne(ctx context.Context, item domain.SectionContent) (string, error) {
	user := "Mục: " + item.Name + "\n\nNội dung:\n" + item.Text
	out, err := s.generate(ctx, s.system, user)
	if err != nil {
		return "", fmt.Errorf("%s (mục %q)", err, item.Name)
	}
	return out, nil
}

// generate gọi model 1 lượt — system + user, temperature 0. Model lỗi hoặc
// trả lời rỗng thì lỗi để service đánh dấu bài failed/backoff, KHÔNG im lặng
// ghi summary trống (compile xong mà rỗng thì không phân biệt được với bài
// chưa compile).
func (s *SectionSummarizer) generate(ctx context.Context, system, user string) (string, error) {
	out, err := s.model.Generate(ctx,
		[]*schema.Message{
			schema.SystemMessage(system),
			schema.UserMessage(user),
		},
		einomodel.WithTemperature(0))
	if err != nil {
		return "", fmt.Errorf("eino summarizer: %w", err)
	}
	if out == nil || strings.TrimSpace(out.Content) == "" {
		return "", fmt.Errorf("eino summarizer: model trả lời rỗng")
	}
	return strings.TrimSpace(out.Content), nil
}

// summarizeEach gọi LLM từng mục — đường dự phòng khi model không theo khuôn
// đầu ra của lô. Tốn N lượt nhưng vẫn ra đủ N tóm tắt.
func (s *SectionSummarizer) summarizeEach(ctx context.Context, items []domain.SectionContent) ([]string, error) {
	out := make([]string, 0, len(items))
	for _, item := range items {
		sum, err := s.summarizeOne(ctx, item)
		if err != nil {
			return nil, err
		}
		out = append(out, sum)
	}
	return out, nil
}

// batchUserMessage ghép N mục thành 1 user message — mỗi mục 1 khối có nhãn
// "Mục <số>:" để model đối chiếu thứ tự khi trả lời.
func batchUserMessage(items []domain.SectionContent) string {
	var b strings.Builder
	for i, item := range items {
		fmt.Fprintf(&b, "Mục %d: %s\n\nNội dung:\n%s\n\n", i+1, item.Name, item.Text)
	}
	return b.String()
}

// batchMarkerRe bắt nhãn [S<i>] mà model phải trả về cho lô mục.
var batchMarkerRe = regexp.MustCompile(`(?m)^\s*\[S(\d+)\]\s*`)

// parseBatchSummaries tách N tóm tắt theo nhãn [S<i>] — trả nil nếu thiếu
// mục nào (model không theo khuôn) để caller gọi lại từng mục.
func parseBatchSummaries(text string, n int) []string {
	matches := batchMarkerRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, n)
	for i, m := range matches {
		idx, err := strconv.Atoi(text[m[2]:m[3]])
		if err != nil || idx < 1 || idx > n {
			continue
		}
		end := len(text)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		out[idx-1] = strings.TrimSpace(text[m[1]:end])
	}
	for _, sum := range out {
		if sum == "" {
			return nil
		}
	}
	return out
}
