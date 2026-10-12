package question

import (
	"context"
	"strings"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"knowledge_ingestion/src/agent/prompts"
)

// QuestionType là 1 trong 8 loại câu hỏi theo
// technical_debt/AGENT_LLM_INSTRUCTIONS.md §5 — vòng 1 của agent dùng để chọn
// tool + tham số search, không phải để từ chối user.
type QuestionType int

const (
	QuestionLookup    QuestionType = 1 // tra cứu fact đơn
	QuestionSummary   QuestionType = 2 // tóm tắt toàn bài
	QuestionCompare   QuestionType = 3 // so sánh 2+ đối tượng
	QuestionProcedure QuestionType = 4 // quy trình/thủ tục nhiều bước
	QuestionReasoning QuestionType = 5 // suy luận đa đoạn, bắc cầu thời gian
	QuestionCalc      QuestionType = 6 // tính toán trên số trong tài liệu
	QuestionChitchat  QuestionType = 7 // chào hỏi, không cần search
	QuestionScenario  QuestionType = 8 // tình huống "nếu/tôi/công ty tôi..."
)

// allQuestionTypes liệt kê đủ 8 loại cho parser quét slug trong câu trả lời.
var allQuestionTypes = []QuestionType{
	QuestionLookup, QuestionSummary, QuestionCompare, QuestionProcedure,
	QuestionReasoning, QuestionCalc, QuestionChitchat, QuestionScenario,
}

// String trả về slug để log + response debug (không phải enum số khó đọc).
func (t QuestionType) String() string {
	switch t {
	case QuestionLookup:
		return "lookup"
	case QuestionSummary:
		return "summary"
	case QuestionCompare:
		return "compare"
	case QuestionProcedure:
		return "procedure"
	case QuestionReasoning:
		return "reasoning"
	case QuestionCalc:
		return "calc"
	case QuestionChitchat:
		return "chitchat"
	case QuestionScenario:
		return "scenario"
	default:
		return "lookup"
	}
}

// Prompts gom prompt vòng 1 load từ file lúc startup — cùng quy ước với
// synthesis: sửa file restart là ăn, không bản mặc định trong code.
type Prompts struct {
	// ClassifySystem thay system prompt phân loại câu hỏi.
	ClassifySystem string
}

// classifySystemFile là tên file system prompt vòng 1 trong thư mục prompt.
const classifySystemFile = "classify_system.md"

// LoadPrompts đọc prompt vòng 1 từ dir lúc startup — ủy quyền cho package
// prompts dùng chung (cùng quy ước fail-fast với prompt chat).
func LoadPrompts(dir string) (Prompts, error) {
	system, err := prompts.Load(dir, classifySystemFile)
	if err != nil {
		return Prompts{}, err
	}
	return Prompts{ClassifySystem: system}, nil
}

// Parse đổi slug thành loại câu hỏi — khớp chính xác trước (slug DTO caller
// gửi), rồi quét chuỗi con (LLM đôi khi trả "Loại: compare" thay vì đúng 1
// từ). Rỗng/lạ thì ok=false để caller classify lại, không đoán bừa.
func Parse(s string) (QuestionType, bool) {
	slug := strings.ToLower(strings.TrimSpace(s))
	if slug == "" {
		return QuestionLookup, false
	}
	for _, t := range allQuestionTypes {
		if slug == t.String() {
			return t, true
		}
	}
	for _, t := range allQuestionTypes {
		if strings.Contains(slug, t.String()) {
			return t, true
		}
	}
	return QuestionLookup, false
}

// LLMClassifier dán nhãn loại câu hỏi bằng ChatModel Eino (chung 1 instance
// Ollama với agent chat) — model.WithModel truyền model theo request, không
// kẹt model trong constructor. Chỉ hỏi 1 câu system+user rồi parse slug.
type LLMClassifier struct {
	model  einomodel.BaseChatModel
	system string
}

func NewLLMClassifier(model einomodel.BaseChatModel, p Prompts) *LLMClassifier {
	return &LLMClassifier{model: model, system: strings.TrimSpace(p.ClassifySystem)}
}

// Classify gọi LLM dán nhãn loại câu hỏi — model rỗng/query rỗng/LLM lỗi/trả
// lời không parse được đều về lookup (loại phổ biến nhất). Không bao giờ trả
// lỗi để caller luôn có đường đi.
func (c *LLMClassifier) Classify(ctx context.Context, model, query string) QuestionType {
	if c == nil || c.model == nil || strings.TrimSpace(c.system) == "" {
		return QuestionLookup
	}
	if strings.TrimSpace(model) == "" || strings.TrimSpace(query) == "" {
		return QuestionLookup
	}
	out, err := c.model.Generate(ctx,
		[]*schema.Message{
			schema.SystemMessage(c.system),
			schema.UserMessage(query),
		},
		einomodel.WithModel(model), einomodel.WithTemperature(0))
	if err != nil || out == nil {
		return QuestionLookup
	}
	if t, ok := Parse(out.Content); ok {
		return t
	}
	return QuestionLookup
}
