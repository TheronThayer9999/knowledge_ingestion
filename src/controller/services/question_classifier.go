package services

import (
	"context"
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

// classifierSystem là system prompt vòng 1 — ép LLM chỉ trả 1 slug, kèm 1 dòng
// mô tả mỗi loại + vài ví dụ neo để model nhỏ (7b) phân biệt được lookup với
// calc/scenario. Giữ prompt ngắn để mỗi lượt classify tốn ít token.
const classifierSystem = `Bạn phân loại câu hỏi cho hệ tìm kiếm văn bản hành chính Việt Nam. Chỉ trả về đúng 1 slug, không giải thích:
- lookup: tra cứu 1 thông tin cụ thể (số hiệu, ngày, mức, định nghĩa)
- summary: tóm tắt toàn bộ văn bản
- compare: so sánh 2 đối tượng trở lên
- procedure: hỏi cách làm, thủ tục, các bước, hồ sơ
- reasoning: tại sao, nguyên nhân, mốc thời gian (từ khi nào, đến khi nào), còn/hết hiệu lực, suy luận nhiều đoạn
- calc: tính toán trên con số trong tài liệu
- chitchat: chào hỏi, cảm ơn, hỏi về bot
- scenario: tình huống cá nhân (nếu tôi, công ty tôi, giả sử, trường hợp của) hoặc liệt kê nhiều trường hợp (liệt kê các...)
Ví dụ: "tóm tắt nghị định 123" -> summary; "so sánh A và B" -> compare; "nếu tôi nghỉ việc thì sao" -> scenario; "xin chào" -> chitchat; "mức phạt là gì" -> lookup`

// LLMClassifier là vòng 1 của agent: hỏi LLM xem câu hỏi thuộc loại nào để
// vòng 2 biết gọi tool nào. Chỉ phụ thuộc domain.ILLM nên test fake được,
// đổi Ollama → provider khác không phải sửa.
type LLMClassifier struct {
	llm domain.ILLM
}

func NewLLMClassifier(llm domain.ILLM) *LLMClassifier {
	return &LLMClassifier{llm: llm}
}

// Classify gọi LLM phân loại — model rỗng/query rỗng/LLM lỗi/trả lời không
// parse được đều về lookup (loại phổ biến nhất, search mặc định vẫn đúng).
// Không bao giờ trả lỗi để vòng 2 luôn có đường đi.
func (c *LLMClassifier) Classify(ctx context.Context, model, query string) QuestionType {
	if c == nil || c.llm == nil {
		return QuestionLookup
	}
	if strings.TrimSpace(model) == "" || strings.TrimSpace(query) == "" {
		return QuestionLookup
	}
	out, err := c.llm.Chat(ctx, []domain.ChatMessage{
		{Role: "system", Content: classifierSystem},
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
