package question

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"knowledge_ingestion/src/domain"
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

// LoadPrompts đọc prompt vòng 1 từ dir lúc startup. File thiếu/không đọc
// được/nội dung rỗng thì trả lỗi — app fail-fast vì không có fallback.
func LoadPrompts(dir string) (Prompts, error) {
	path := filepath.Join(dir, classifySystemFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		return Prompts{}, fmt.Errorf("đọc system prompt classify: %w (muốn %s)", err, path)
	}
	system := strings.TrimSpace(string(raw))
	if system == "" {
		return Prompts{}, fmt.Errorf("system prompt classify rỗng: %s", path)
	}
	return Prompts{ClassifySystem: system}, nil
}

// Parse đổi slug thành loại câu hỏi — chat vòng 1 classify xong truyền sang
// search qua DTO để search khỏi tốn thêm lượt LLM. Rỗng/lạ thì ok=false để
// caller classify lại, không đoán bừa.
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
	return QuestionLookup, false
}

// LLMClassifier là vòng 1 của agent: hỏi LLM xem câu hỏi thuộc loại nào để
// vòng 2 biết gọi tool nào. Chỉ phụ thuộc domain.ILLM nên test fake được,
// đổi Ollama → provider khác không phải sửa.
type LLMClassifier struct {
	llm    domain.ILLM
	system string
}

func NewLLMClassifier(llm domain.ILLM, p Prompts) *LLMClassifier {
	return &LLMClassifier{llm: llm, system: strings.TrimSpace(p.ClassifySystem)}
}

// Classify gọi LLM phân loại — model rỗng/query rỗng/LLM lỗi/trả lời không
// parse được đều về lookup (loại phổ biến nhất, search mặc định vẫn đúng).
// Không bao giờ trả lỗi để vòng 2 luôn có đường đi.
func (c *LLMClassifier) Classify(ctx context.Context, model, query string) QuestionType {
	if c == nil || c.llm == nil || strings.TrimSpace(c.system) == "" {
		return QuestionLookup
	}
	if strings.TrimSpace(model) == "" || strings.TrimSpace(query) == "" {
		return QuestionLookup
	}
	out, err := c.llm.Chat(ctx, []domain.ChatMessage{
		{Role: "system", Content: c.system},
		{Role: "user", Content: query},
	}, domain.ChatOptions{Model: model, Temperature: 0})
	if err != nil {
		return QuestionLookup
	}
	return parseQuestionType(out)
}

// parseQuestionType quét slug trong câu trả lời — LLM đôi khi trả "Loại:
// compare" thay vì đúng 1 từ nên quét chuỗi con thay vì so bằng.
func parseQuestionType(s string) QuestionType {
	slug := strings.ToLower(strings.TrimSpace(s))
	for _, t := range allQuestionTypes {
		if strings.Contains(slug, t.String()) {
			return t
		}
	}
	return QuestionLookup
}
