package eino

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"

	"knowledge_ingestion/src/agent/retrieval"
	"knowledge_ingestion/src/agent/tools"
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

// KnowledgeSearchName là tên tool duy nhất LLM thấy — trùng tools.Registry để
// log và test nói cùng một tên.
const KnowledgeSearchName = tools.KnowledgeSearch

// KnowledgeSearchArgs là args LLM được phép đưa — query bắt buộc; category_ids
// và limit là gợi ý, phạm vi tin cậy (user, category ép, threshold) nằm ở
// Scope trong ctx, không tin LLM.
type KnowledgeSearchArgs struct {
	Query       string  `json:"query" jsonschema:"description=Câu hỏi hoặc từ khóa cần tìm trong tài liệu,required"`
	CategoryIDs []int64 `json:"category_ids,omitempty" jsonschema:"description=ID các chủ đề muốn khoanh, rỗng là tìm mọi chủ đề"`
	Limit       int     `json:"limit,omitempty" jsonschema:"description=Số đoạn cần đọc: tra cứu 5, quy trình 8, tóm tắt so sánh suy luận 10"`
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

const knowledgeSearchDesc = `Tìm đoạn trích trong tài liệu người dùng đã upload. Dùng khi câu hỏi cần căn cứ từ tài liệu (tra cứu, tóm tắt, so sánh, quy trình, suy luận, tính toán, tình huống). KHÔNG gọi cho chào hỏi, cảm ơn, trò chuyện. Kết quả rỗng hoặc chưa đủ căn cứ thì đổi từ khóa gọi lại (tối đa 1-2 lần). Kết quả trả về là JSON gồm n (số thứ tự nguồn), article_id, page_num, chunk_index, text — khi trả lời PHẢI trích dẫn bằng [^n] khớp số này, đặt ngay sau câu dùng thông tin đó.`

// NewKnowledgeSearchTool bọc HybridSearcher thành Eino InvokableTool — gọi
// thẳng pipeline search, limit LLM gợi ý được kẹp trần ở 1 chỗ duy nhất.
func NewKnowledgeSearchTool(hybrid *retrieval.HybridSearcher) (einotool.InvokableTool, error) {
	if hybrid == nil {
		return nil, fmt.Errorf("eino knowledge_search: searcher is required")
	}
	return toolutils.InferTool(KnowledgeSearchName, knowledgeSearchDesc,
		func(ctx context.Context, in KnowledgeSearchArgs) (string, error) {
			scope, ok := ScopeFrom(ctx)
			if !ok {
				return "", fmt.Errorf("eino knowledge_search: thiếu scope request (user_id) trong ctx")
			}
			if strings.TrimSpace(in.Query) == "" {
				return "", fmt.Errorf("eino knowledge_search: query rỗng")
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
				Limit:          tools.ResolveLimit(in.Limit, 0),
				ScoreThreshold: scope.ScoreThreshold,
			})
			if err != nil {
				return "", err
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
			raw, err := json.Marshal(out)
			if err != nil {
				return "", fmt.Errorf("eino knowledge_search: marshal hits: %w", err)
			}
			return string(raw), nil
		})
}
