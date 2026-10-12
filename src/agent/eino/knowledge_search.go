package eino

import (
	"context"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"

	"knowledge_ingestion/src/agent/retrieval"
)

// Thêm tool mới cho agent chỉ cần 3 bước, không đụng usecase/prompt:
//  1. Tạo 1 file trong package này: struct Args (field nào LLM được truyền thì
//     khai báo + tag jsonschema mô tả), 1 const desc (khi nào gọi/không gọi,
//     định dạng kết quả trả về), 1 constructor NewXTool bọc InferTool.
//     Desc + schema args là nguồn thật duy nhất LLM thấy — viết cho LLM đọc,
//     đừng ghi tóm tắt ở prompt file.
//  2. Đăng ký 1 dòng trong loader.newChatAgent.
//  3. Phạm vi tin cậy (user, category ép) đọc qua ScopeFrom(ctx), không tin
//     args LLM. Kết quả nhiều vòng gom qua scope.Collector để cites khớp [^n].

// KnowledgeSearchName là tên tool duy nhất LLM thấy.
const KnowledgeSearchName = "knowledge_search"

// KnowledgeSearchArgs là args LLM được phép đưa — query bắt buộc; category_ids
// và limit là gợi ý, phạm vi tin cậy (user, category ép, threshold) nằm ở
// Scope trong ctx, không tin LLM.
type KnowledgeSearchArgs struct {
	Query string `json:"query" jsonschema:"description=Cụm từ khóa bằng tiếng Việt để tìm trong tài liệu. Giữ nguyên tên văn bản và số hiệu và thuật ngữ trong câu hỏi. Đừng rút gọn làm mất nghĩa.,required"`
	// LLM không bao giờ biết ID chủ đề hợp lệ (không có tool nào liệt kê) nên
	// desc cấm bịa ID — chỉ truyền khi user nêu rõ, còn lại bỏ trống.
	CategoryIDs []int64 `json:"category_ids,omitempty" jsonschema:"description=Chỉ truyền khi người dùng nêu rõ ID chủ đề trong câu hỏi. Không tự bịa ID. Không biết thì bỏ trống để tìm mọi chủ đề."`
	Limit       int     `json:"limit,omitempty" jsonschema:"description=Số đoạn cần đọc. Hỏi một thông tin cụ thể thì 5. Quy trình nhiều bước thì 8. Tóm tắt hoặc so sánh hoặc suy luận thì 10. Bỏ trống thì mặc định 5."`
}

// knowledgeSearchHit là 1 hit trả cho LLM — n khớp HitCollector để cites.
type knowledgeSearchHit struct {
	N          int     `json:"n"`
	ArticleID  int64   `json:"article_id"`
	PageNum    int     `json:"page_num"`
	ChunkIndex int     `json:"chunk_index"`
	Score      float32 `json:"score"`
	Text       string  `json:"text"`
}

const knowledgeSearchDesc = `Tìm đoạn trích trong các tài liệu mà NGƯỜI DÙNG ĐANG HỎI đã upload (không phải tài liệu công khai trên mạng).

KHI NÀO GỌI: mọi câu hỏi cần căn cứ từ tài liệu — hỏi một thông tin cụ thể, tóm tắt, so sánh, quy trình nhiều bước, suy luận từ nhiều đoạn, tính toán trên số liệu trong tài liệu, áp dụng vào trường hợp "nếu/tôi/công ty tôi...".

KHI NÀO KHÔNG GỌI: chào hỏi, cảm ơn, trò chuyện xã giao; câu hỏi kiến thức chung không dính gì đến tài liệu. Những trường hợp này đáp thẳng, không gọi tool.

CÁCH ĐẶT QUERY: viết bằng tiếng Việt, giữ nguyên các từ khóa quan trọng trong câu hỏi (tên văn bản, số hiệu, thuật ngữ chuyên ngành) kèm theo ý hỏi. Ví dụ câu "mức phạt chậm nộp thuế là bao nhiêu" thì query là "mức phạt chậm nộp thuế". Đừng rút gọn thành 1-2 từ mất nghĩa, cũng đừng bê nguyên cả câu dài dòng.

KẾT QUẢ TRẢ VỀ: JSON gồm các phần tử {n, article_id, page_num, chunk_index, score, text}. n là số thứ tự nguồn — khi trả lời PHẢI trích dẫn bằng [^n] khớp đúng số này, đặt ngay sau câu dùng thông tin đó; chỉ dùng số n có trong kết quả, không bịa số. score chỉ để tham khảo độ liên quan, KHÔNG nhắc đến trong câu trả lời.

NẾU KẾT QUẢ RỖNG HOẶC CHƯA ĐỦ CĂN CỨ: đổi từ khóa (dùng từ đồng nghĩa, tên văn bản hoặc số hiệu khác) rồi gọi lại, tối đa 1-2 lần. Vẫn không có thì kết luận không tìm thấy trong tài liệu, không đoán.`

// NewKnowledgeSearchTool bọc HybridSearcher thành Eino InvokableTool — gọi
// thẳng pipeline search. Trả slice struct để InferTool tự encode JSON (đừng
// Marshal tay thành string). Limit LLM gợi ý được HybridSearcher kẹp trần,
// category DTO ép thắng gợi ý LLM.
func NewKnowledgeSearchTool(hybrid *retrieval.HybridSearcher) (einotool.InvokableTool, error) {
	if hybrid == nil {
		return nil, fmt.Errorf("eino knowledge_search: searcher is required")
	}
	return toolutils.InferTool(KnowledgeSearchName, knowledgeSearchDesc,
		func(ctx context.Context, in KnowledgeSearchArgs) ([]knowledgeSearchHit, error) {
			scope, ok := ScopeFrom(ctx)
			if !ok {
				return nil, fmt.Errorf("eino knowledge_search: thiếu scope request (user_id) trong ctx")
			}
			if strings.TrimSpace(in.Query) == "" {
				return nil, fmt.Errorf("eino knowledge_search: query rỗng")
			}
			// Category DTO ép thắng gợi ý LLM — LLM không được mở rộng phạm
			// vi ra ngoài các chủ đề user đã khoanh.
			ids := in.CategoryIDs
			if len(scope.CategoryIDs) > 0 {
				ids = scope.CategoryIDs
			}
			chunks, err := hybrid.SearchHybrid(ctx, retrieval.HybridParams{
				UserID:         scope.UserID,
				Query:          in.Query,
				CategoryIDs:    ids,
				Limit:          in.Limit,
				ScoreThreshold: scope.ScoreThreshold,
			})
			if err != nil {
				return nil, err
			}
			var ns []int
			if scope.Collector != nil {
				ns = scope.Collector.Add(chunks)
			} else {
				ns = make([]int, len(chunks))
				for i := range chunks {
					ns[i] = i + 1
				}
			}
			out := make([]knowledgeSearchHit, 0, len(chunks))
			for i, h := range chunks {
				if h == nil || ns[i] <= 0 {
					continue
				}
				out = append(out, knowledgeSearchHit{
					N:          ns[i],
					ArticleID:  h.ArticleID,
					PageNum:    h.PageNum,
					ChunkIndex: h.ChunkIndex,
					Score:      h.Score,
					Text:       h.Text,
				})
			}
			return out, nil
		})
}
