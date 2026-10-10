package services

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

// IChatService hỏi đáp tri thức cho frontend chat — BE điều phối agentic
// (vòng 1 classify + vòng 2 tool search + tổng hợp), frontend ReactJS chỉ
// render markdown + map cites, không cầm logic agent.
type IChatService interface {
	// Chat chạy full pipeline: search (2 vòng agentic) rồi LLM tổng hợp thành
	// markdown có chú thích [^n]. Model bắt buộc vì không có model thì không
	// tổng hợp được.
	Chat(ctx context.Context, dto *dtos.ChatRequest) dtos.Result[*dtos.ChatResponse]
}

type chatService struct {
	search ISearchService
	llm    domain.ILLM
}

func NewChatService(search ISearchService, llm domain.ILLM) IChatService {
	return &chatService{search: search, llm: llm}
}

// synthSystem là system prompt vòng tổng hợp — ép Markdown + chú thích [^n]
// khớp cites backend đánh số, để ReactJS render thẳng không cần parse lại.
const synthSystem = `Bạn là trợ lý tri thức, trả lời dựa trên NGUỒN được đánh số dưới đây (lấy từ tài liệu người dùng đã upload).
1. Trả lời bằng Markdown: tiêu đề, danh sách, in đậm cho số liệu quan trọng.
2. Mọi khẳng định từ tài liệu kèm chú thích [^n] ứng với số nguồn (vd "mức phạt 0.05%/ngày[^1]"). Không tự bịa số nguồn.
3. Kiến thức của bạn chỉ dùng để diễn đạt lại, cấm bổ sung sự thật mới. Nguồn mâu thuẫn thì nêu rõ mâu thuẫn.
4. Không có nguồn nào liên quan: nói "không tìm thấy trong tài liệu đã upload" và gợi ý từ khóa khác — trừ chào hỏi/cảm ơn thì đáp ngắn thân thiện, mời hỏi về tài liệu, không cần nguồn.
5. Trả lời bằng tiếng Việt. Không nhắc score, point ID, tên tool/collection.`

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

// snippetLen là độ dài trích đoạn mỗi cite — đủ để frontend hiện preview,
// không bê nguyên chunk dài vào response.
const snippetLen = 200

// snippet cắt text theo rune (không cắt giữa ký tự Việt) cho preview cite.
func snippet(text string) string {
	if utf8.RuneCountInString(text) <= snippetLen {
		return text
	}
	return string([]rune(text)[:snippetLen]) + "…"
}

func (s *chatService) Chat(ctx context.Context, dto *dtos.ChatRequest) dtos.Result[*dtos.ChatResponse] {
	if strings.TrimSpace(dto.Message) == "" {
		return dtos.Fail[*dtos.ChatResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "message rỗng"))
	}
	if strings.TrimSpace(dto.Model) == "" {
		return dtos.Fail[*dtos.ChatResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "model bắt buộc (chat cần LLM để tổng hợp)"))
	}
	if s.llm == nil {
		return dtos.Fail[*dtos.ChatResponse](fmt.Errorf("chat: llm chưa được wire"))
	}

	// Vòng 1+2a: search agentic (classify trong Search khi có model) → hits.
	searchRes := s.search.Search(ctx, &dtos.SearchRequest{
		Query:      dto.Message,
		CategoryID: dto.CategoryID,
		Limit:      dto.Limit,
		Model:      dto.Model,
	})
	if searchRes.Err != nil {
		return dtos.Fail[*dtos.ChatResponse](searchRes.Err)
	}
	hits := searchRes.Data.Hits
	qtype := searchRes.Data.QuestionType

	// Vòng 2b: dựng nguồn đánh số + lịch sử rồi gọi LLM tổng hợp markdown.
	var src strings.Builder
	for i, h := range hits {
		fmt.Fprintf(&src, "[%d] (trang %d, đoạn %d)\n%s\n\n", i+1, h.PageNum, h.ChunkIndex, h.Text)
	}
	var hist strings.Builder
	for _, m := range dto.History {
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		role := "người hỏi"
		if m.Role == "assistant" {
			role = "trợ lý"
		}
		fmt.Fprintf(&hist, "%s: %s\n", role, m.Content)
	}
	userContent := dto.Message
	if hint, ok := synthHint[qtype]; ok && hint != "" {
		userContent += "\n(Gợi ý trình bày: " + hint + ")"
	}
	if hist.Len() > 0 {
		userContent = "Hội thoại trước:\n" + hist.String() + "\nCâu hỏi hiện tại: " + userContent
	}
	if src.Len() > 0 {
		userContent += "\n\nNGUỒN:\n" + src.String()
	} else {
		userContent += "\n\n(Không có nguồn nào — làm đúng rule 4 của system prompt.)"
	}

	answer, err := s.llm.Chat(ctx, []domain.ChatMessage{
		{Role: "system", Content: synthSystem},
		{Role: "user", Content: userContent},
	}, domain.ChatOptions{Model: dto.Model, Temperature: 0})
	if err != nil {
		return dtos.Fail[*dtos.ChatResponse](fmt.Errorf("chat: tổng hợp: %w", err))
	}

	cites := make([]*dtos.ChatCite, 0, len(hits))
	for i, h := range hits {
		cites = append(cites, &dtos.ChatCite{
			N:          i + 1,
			ArticleID:  h.ArticleID,
			PageNum:    h.PageNum,
			ChunkIndex: h.ChunkIndex,
			Snippet:    snippet(h.Text),
		})
	}
	return dtos.Ok(&dtos.ChatResponse{Answer: answer, QuestionType: qtype, Cites: cites})
}
