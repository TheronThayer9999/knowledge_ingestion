package synthesis

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

// IChatSynthesizer gom toàn bộ việc liên quan đến LLM của chat: dựng prompt
// từ hits + history rồi gọi ILLM tổng hợp thành markdown. Nằm trong agent để
// chatService chỉ còn điều phối search → synthesize → cites.
type IChatSynthesizer interface {
	// Synthesize tổng hợp câu trả lời từ hits search + lịch sử hội thoại.
	Synthesize(ctx context.Context, model, question, qtype string, hits []*dtos.SearchHitResponse, history []dtos.ChatHistoryItem) (string, error)
}

// chatSynthesizer là implement mặc định của IChatSynthesizer bằng domain.ILLM.
type chatSynthesizer struct {
	llm    domain.ILLM
	system string
}

// NewChatSynthesizer dựng synthesizer với system prompt đã load từ file
// (prompts/chat_system.md) — sửa prompt rồi restart app là ăn, không cần
// build lại. Không còn bản mặc định trong code: system rỗng thì Synthesize
// lỗi rõ thay vì trả lời bừa không prompt.
func NewChatSynthesizer(llm domain.ILLM, p Prompts) IChatSynthesizer {
	return &chatSynthesizer{llm: llm, system: strings.TrimSpace(p.System)}
}

// Prompts gom prompt load từ file lúc startup — sửa file, restart app là ăn,
// không cần build lại. Không có bản mặc định trong code.
type Prompts struct {
	// System thay system prompt vòng tổng hợp chat.
	System string
}

// chatSystemFile là tên file system prompt chat trong thư mục prompt.
const chatSystemFile = "chat_system.md"

// LoadPrompts đọc prompt từ dir lúc startup. File thiếu/không đọc được/nội
// dung rỗng thì trả lỗi — prompt là bắt buộc nên app fail-fast thay vì chạy
// với câu trả lời không prompt.
func LoadPrompts(dir string) (Prompts, error) {
	path := filepath.Join(dir, chatSystemFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		return Prompts{}, fmt.Errorf("đọc system prompt chat: %w (muốn %s)", err, path)
	}
	system := strings.TrimSpace(string(raw))
	if system == "" {
		return Prompts{}, fmt.Errorf("system prompt chat rỗng: %s", path)
	}
	return Prompts{System: system}, nil
}

// synthHint là gợi ý trình bày theo loại câu hỏi (bản rút gọn của §5 — bản
// đầy đủ nằm ở AGENT_LLM_INSTRUCTIONS.md cho agent Eino sau này).
var synthHint = map[string]string{
	"lookup":    "Trả lời trực tiếp 1-2 câu, quote nguyên văn đoạn chứa sự thật.",
	"summary":   "Tóm tắt theo mạch văn bản, mỗi ý 1-2 dòng kèm chú thích.",
	"compare":   "Trình bày dạng bảng 2 cột (tiêu chí | bên A | bên B) + 1 dòng kết luận giống/khác.",
	"procedure": "Trả lời dạng Bước 1-2-3 theo đúng thứ tự.",
	"reasoning": "Viết kết luận trước, chứng cứ sau, nêu rõ điều kiện/ngoại lệ.",
	"calc":      "Trình bày phép tính minh bạch từng dòng (toán hạng + chú thích) rồi mới ra kết quả.",
	"chitchat":  "Đáp ngắn thân thiện, mời hỏi về tài liệu; không cần nguồn.",
	"scenario":  "Trình bày facts đã xác lập → điều kiện → tính toán → kết luận cho trường hợp cụ thể.",
}

// Synthesize dựng user prompt (câu hỏi + gợi ý trình bày + history + nguồn
// đánh số) rồi gọi LLM. Model rỗng hoặc llm chưa wire thì lỗi ngay để caller
// map thành 400/500 thay vì trả lời bừa.
func (s *chatSynthesizer) Synthesize(ctx context.Context, model, question, qtype string, hits []*dtos.SearchHitResponse, history []dtos.ChatHistoryItem) (string, error) {
	if s == nil || s.llm == nil {
		return "", fmt.Errorf("chat: llm chưa được wire")
	}
	if strings.TrimSpace(model) == "" {
		return "", fmt.Errorf("chat: model bắt buộc (chat cần LLM để tổng hợp)")
	}

	userContent := buildSynthUserContent(question, qtype, hits, history)
	if strings.TrimSpace(s.system) == "" {
		return "", fmt.Errorf("chat: thiếu system prompt (prompts/chat_system.md chưa load)")
	}
	answer, err := s.llm.Chat(ctx, []domain.ChatMessage{
		{Role: "system", Content: s.system},
		{Role: "user", Content: userContent},
	}, domain.ChatOptions{Model: model, Temperature: 0})
	if err != nil {
		return "", fmt.Errorf("chat: tổng hợp: %w", err)
	}
	return answer, nil
}

// buildSynthUserContent ghép câu hỏi + hint trình bày + hội thoại trước +
// nguồn đánh số [n] thành 1 user prompt duy nhất cho LLM.
func buildSynthUserContent(question, qtype string, hits []*dtos.SearchHitResponse, history []dtos.ChatHistoryItem) string {
	var src strings.Builder
	for i, h := range hits {
		fmt.Fprintf(&src, "[%d] (trang %d, đoạn %d)\n%s\n\n", i+1, h.PageNum, h.ChunkIndex, h.Text)
	}
	var hist strings.Builder
	for _, m := range history {
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		role := "người hỏi"
		if m.Role == "assistant" {
			role = "trợ lý"
		}
		fmt.Fprintf(&hist, "%s: %s\n", role, m.Content)
	}

	userContent := question
	if hint, ok := synthHint[qtype]; ok && hint != "" {
		userContent += "\n(Gợi ý trình bày: " + hint + ")"
	}
	if hist.Len() > 0 {
		userContent = "Hội thoại trước:\n" + hist.String() + "\nCâu hỏi hiện tại: " + userContent
	}
	if src.Len() > 0 {
		userContent += "\n\nNGUỒN:\n" + src.String()
	} else {
		userContent += "\n\n(Không có nguồn nào — làm đúng mục 8 của system prompt.)"
	}
	return userContent
}
