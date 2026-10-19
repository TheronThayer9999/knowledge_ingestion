package eino

import (
	"context"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"

	"knowledge_ingestion/src/agent/retrieval"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/domain"
)

// Thêm tool mới cho agent chỉ cần 3 bước, không đụng usecase/prompt — xem
// hướng dẫn và ví dụ MyInput/MyOutput ở chaintool.go (NewChainTool).

// KnowledgeSearchName là tên tool duy nhất LLM thấy.
const KnowledgeSearchName = "knowledge_search"

// KnowledgeSearchArgs là args LLM được phép đưa — query bắt buộc; category_ids
// và limit là gợi ý, phạm vi tin cậy (user, category ép, threshold) nằm ở
// Scope trong ctx, không tin LLM.
type KnowledgeSearchArgs struct {
	Query string `json:"query" jsonschema:"description=Cụm từ khóa bằng tiếng Việt để tìm trong tài liệu. Giữ nguyên tên văn bản và số hiệu và thuật ngữ trong câu hỏi. Đừng rút gọn làm mất nghĩa.,required"`
	// LLM không bao giờ biết ID chủ đề hợp lệ (không có tool nào liệt kê) nên
	// desc cấm bịa ID — chỉ truyền khi user nêu rõ, còn lại bỏ trống.
	CategoryIDs []flexInt64 `json:"category_ids,omitempty" jsonschema:"description=Chỉ truyền khi người dùng nêu rõ ID chủ đề trong câu hỏi. Không tự bịa ID. Không biết thì bỏ trống để tìm mọi chủ đề."`
	Limit       flexInt     `json:"limit,omitempty" jsonschema:"description=Số đoạn cần đọc. Hỏi một thông tin cụ thể thì 5. Quy trình nhiều bước thì 8. Tóm tắt hoặc so sánh hoặc suy luận thì 10. Bỏ trống thì mặc định 5."`
}

// knowledgeSearchHit là 1 hit trả cho LLM — n khớp HitCollector để cites.
type knowledgeSearchHit struct {
	N          int                      `json:"n"`
	ArticleID  int64                    `json:"article_id"`
	PageNum    int                      `json:"page_num"`
	ChunkIndex int                      `json:"chunk_index"`
	Score      float32                  `json:"score"`
	Text       string                   `json:"text"`
	Context    []knowledgeSearchContext `json:"context,omitempty"`
}

// knowledgeSearchContext là chunk kề trước/sau của 1 hit — chỉ để hiểu ngữ
// cảnh, KHÔNG có n và KHÔNG trích dẫn [^n] (cites chỉ đếm hit chính).
type knowledgeSearchContext struct {
	ChunkIndex int    `json:"chunk_index"`
	Section    string `json:"section,omitempty"`
	Text       string `json:"text"`
}

// neighborWindow là số chunk kề lấy thêm mỗi bên của mỗi hit. Chunk ~1000 ký
// tự nên 1 bên đã đủ câu/khoản bị cắt; mở rộng hơn làm context phình nhanh.
const neighborWindow = 1

// knowledgeSearchResult là kết quả tool trả LLM — bọc hits bằng message để
// câu trả lời tool luôn có lời (kể cả khi query rỗng hoặc không tìm thấy),
// model tự hiểu mà không cần hard-error làm rã cả lượt (NodeRunError → 500).
type knowledgeSearchResult struct {
	Message string               `json:"message"`
	Hits    []knowledgeSearchHit `json:"hits"`
}

const knowledgeSearchDesc = `Tìm đoạn trích trong các tài liệu mà NGƯỜI DÙNG ĐANG HỎI đã upload (không phải tài liệu công khai trên mạng).

KHI NÀO GỌI: mặc định mọi câu hỏi có nội dung tri thức (khái niệm, định nghĩa, con số, điều khoản, tên riêng, sự kiện, nguyên lý, thuật ngữ...) — kể cả câu nghe như kiến thức phổ thông — đều phải gọi trước khi trả lời. Hỏi một thông tin cụ thể, tóm tắt, so sánh, quy trình nhiều bước, suy luận từ nhiều đoạn, tính toán trên số liệu trong tài liệu, áp dụng vào trường hợp "nếu/tôi/công ty tôi...".

KHI NÀO KHÔNG GỌI: chỉ chào hỏi, cảm ơn, xưng hô thuần túy không kèm nội dung tri thức. Khi không chắc câu hỏi có dính đến tài liệu hay không, luôn gọi một lần trước rồi mới kết luận. Những trường hợp đáp thẳng này không gọi tool.

CÁCH ĐẶT QUERY: viết bằng tiếng Việt, giữ nguyên các từ khóa quan trọng trong câu hỏi (tên văn bản, số hiệu, thuật ngữ chuyên ngành) kèm theo ý hỏi. Ví dụ câu "mức phạt chậm nộp thuế là bao nhiêu" thì query là "mức phạt chậm nộp thuế". Đừng rút gọn thành 1-2 từ mất nghĩa, cũng đừng bê nguyên cả câu dài dòng.

KẾT QUẢ TRẢ VỀ: object {message, hits} — message là lời của công cụ (gợi ý cách sửa query khi rỗng hoặc không tìm thấy), hits là mảng JSON gồm các phần tử {n, article_id, page_num, chunk_index, score, text, context}. n là số thứ tự nguồn — khi trả lời PHẢI trích dẫn bằng [^n] khớp đúng số này, đặt ngay sau câu dùng thông tin đó; chỉ dùng số n có trong hits, không bịa số. score chỉ để tham khảo độ liên quan, KHÔNG nhắc đến trong câu trả lời. context là đoạn kề trước/sau của đúng đoạn đó, đã có sẵn để hiểu đoạn bị cắt (ví dụ tên Điều/Khoản hoặc vế còn lại của quy định); context KHÔNG có n và KHÔNG trích dẫn bằng [^n], chỉ dùng để hiểu nội dung của đoạn n.

NẾU KẾT QUẢ RỖNG HOẶC CHƯA ĐỦ CĂN CỨ: đổi từ khóa (dùng từ đồng nghĩa, tên văn bản hoặc số hiệu khác) rồi gọi lại, tối đa 1-2 lần. Vẫn không có thì kết luận không tìm thấy trong tài liệu, không đoán.`

// neighborsOf lấy chunk kề (window neighborWindow) cho từng hit, aligned theo
// index với chunks. Bỏ chunk kề đã là hit khác (tránh lặp) và cache theo
// (bài, chunk) để hit cùng tâm không gọi Qdrant lại. Lỗi hạ tầng chỉ bỏ context
// của hit đó, không làm hỏng cả lượt search.
func neighborsOf(ctx context.Context, vectors domain.IVectorStore, scope Scope, ids []int64, chunks []*domain.ScoredChunk) [][]knowledgeSearchContext {
	out := make([][]knowledgeSearchContext, len(chunks))
	hitKeys := make(map[[2]int64]bool, len(chunks))
	for _, h := range chunks {
		if h != nil {
			hitKeys[[2]int64{h.ArticleID, int64(h.ChunkIndex)}] = true
		}
	}
	cache := make(map[[2]int64][]*domain.ScoredChunk)
	for i, h := range chunks {
		if h == nil {
			continue
		}
		key := [2]int64{h.ArticleID, int64(h.ChunkIndex)}
		near, ok := cache[key]
		if !ok {
			var err error
			near, err = vectors.ScrollNeighbors(ctx, domain.SearchFilter{
				UserID:      scope.UserID,
				CategoryIDs: ids,
				ArticleID:   h.ArticleID,
			}, h.ChunkIndex, neighborWindow)
			if err != nil {
				logs.Warnw("knowledge_search: lấy chunk kề lỗi, bỏ context của hit", "article_id", h.ArticleID, "chunk_index", h.ChunkIndex, "error", err)
				near = nil
			}
			cache[key] = near
		}
		for _, n := range near {
			if n == nil || n.ChunkIndex == h.ChunkIndex {
				continue
			}
			if hitKeys[[2]int64{n.ArticleID, int64(n.ChunkIndex)}] {
				continue
			}
			out[i] = append(out[i], knowledgeSearchContext{ChunkIndex: n.ChunkIndex, Section: n.Section, Text: n.Text})
		}
	}
	return out
}

// NewKnowledgeSearchTool bọc HybridSearcher thành Eino InvokableTool — gọi
// thẳng pipeline search. Trả slice struct để InferTool tự encode JSON (đừng
// Marshal tay thành string). Limit LLM gợi ý được HybridSearcher kẹp trần,
// category DTO ép thắng gợi ý LLM.
func NewKnowledgeSearchTool(hybrid *retrieval.HybridSearcher, vectors domain.IVectorStore) (einotool.InvokableTool, error) {
	if hybrid == nil {
		return nil, fmt.Errorf("eino knowledge_search: searcher is required")
	}
	if vectors == nil {
		return nil, fmt.Errorf("eino knowledge_search: vector store is required")
	}
	return toolutils.InferTool(KnowledgeSearchName, knowledgeSearchDesc,
		func(ctx context.Context, in KnowledgeSearchArgs) (knowledgeSearchResult, error) {
			scope, ok := ScopeFrom(ctx)
			if !ok {
				return knowledgeSearchResult{}, fmt.Errorf("eino knowledge_search: thiếu scope request (user_id) trong ctx")
			}
			// Query rỗng là lỗi soft — trả message bảo model tự sửa thay vì
			// error làm rã cả lượt (NodeRunError → 500 cho cả request chat).
			if strings.TrimSpace(in.Query) == "" {
				return knowledgeSearchResult{
					Message: "query rỗng — hãy gọi lại với query là cụm từ khóa tiếng Việt giữ nguyên thuật ngữ và số hiệu trong câu hỏi của người dùng.",
				}, nil
			}
			// Category DTO ép thắng gợi ý LLM — LLM không được mở rộng phạm
			// vi ra ngoài các chủ đề user đã khoanh.
			ids := make([]int64, len(in.CategoryIDs))
			for i, id := range in.CategoryIDs {
				ids[i] = int64(id)
			}
			if len(scope.CategoryIDs) > 0 {
				ids = scope.CategoryIDs
			}
			// 0 giữ nguyên để hybrid search tự default (chốt 1 chỗ duy nhất).
			limit := int(in.Limit)
			chunks, err := hybrid.SearchHybrid(ctx, retrieval.HybridParams{
				UserID:         scope.UserID,
				Query:          in.Query,
				CategoryIDs:    ids,
				Limit:          limit,
				ScoreThreshold: scope.ScoreThreshold,
			})
			if err != nil {
				return knowledgeSearchResult{}, err
			}
			// Đã đọc tài liệu dù kết quả rỗng — nhãn response classify thay vì
			// chitchat oan.
			if scope.Collector != nil {
				scope.Collector.MarkSearched()
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
			neighbors := neighborsOf(ctx, vectors, scope, ids, chunks)
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
					Context:    neighbors[i],
				})
			}
			res := knowledgeSearchResult{Message: "Đã tìm trong tài liệu của người dùng.", Hits: out}
			if len(out) == 0 {
				res.Message = "Không tìm thấy đoạn nào khớp — hãy thử cụm từ khóa khác (từ đồng nghĩa, tên văn bản hoặc số hiệu khác) rồi gọi lại, tối đa 1-2 lần; vẫn không có thì kết luận không tìm thấy, không đoán."
			}
			return res, nil
		})
}
