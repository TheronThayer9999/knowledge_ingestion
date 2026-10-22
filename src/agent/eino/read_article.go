package eino

import (
	"context"
	"fmt"
	"sort"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"

	"knowledge_ingestion/src/domain"
)

// ReadArticleName là tên tool duy nhất LLM thấy.
const ReadArticleName = "read_article"

// readArticleDefaultLimit là số chunk 1 trang khi LLM không truyền — 8 chunk
// ~8k ký tự đủ thấy bức tranh cả file mà không phình context.
const readArticleDefaultLimit = 8

// ReadArticleArgs là args LLM được phép đưa — article_id lấy từ
// knowledge_search/article_info, from là chỉ số chunk bắt đầu đọc (luỹ tiến
// các trang sau).
type ReadArticleArgs struct {
	ArticleID flexInt64 `json:"article_id" jsonschema:"description=ID bài viết cần đọc (lấy từ knowledge_search hoặc article_info). Bắt buộc."`
	From      flexInt   `json:"from,omitempty" jsonschema:"description=Chunk bắt đầu đọc (bỏ trống = 0, trang đầu). Trang sau truyền next_from của trang trước."`
	Limit     flexInt   `json:"limit,omitempty" jsonschema:"description=Số chunk mỗi trang (mặc định 8, tối đa 20)."`
}

// readArticlePage là 1 trang nội dung file trả cho LLM — n khớp HitCollector
// cho cites, next_from = 0 thì hết file.
type readArticlePage struct {
	ArticleID  int64  `json:"article_id"`
	From       int    `json:"from"`
	NextFrom   int    `json:"next_from"`
	TotalChunk int64  `json:"total_chunk"`
	Sections   string `json:"sections,omitempty"`
	Items      []struct {
		N          int    `json:"n"`
		ChunkIndex int    `json:"chunk_index"`
		PageNum    int    `json:"page_num"`
		Section    string `json:"section,omitempty"`
		Text       string `json:"text"`
	} `json:"items"`
}

const readArticleDesc = `Đọc TRỰC TIẾP nội dung 1 file theo trang (từ đầu đến cuối, giữ nguyên thứ tự chunk) — khác knowledge_search (trả đoạn rời rạc theo độ tương đồng).

KHI NÀO GỌI: liệt kê toàn bộ nội dung/Điều/khoản của 1 file; tóm tắt cả file; rà soát văn bản từ đầu đến cuối; cần đủ mọi mục không sót (câu "liệt kê tất cả...", "file có những quy định nào...").

KHI NÀO KHÔNG GỌC: hỏi 1 thông tin cụ thể (knowledge_search); hỏi số trang/phiên bản (article_info); đếm số Điều không cần nội dung (list_sections); chào hỏi.

CÁCH DÙNG: biết ID file thì gọi với from=0. Còn trang thì trả về next_from > 0 — truyền next_from vào lần gọi sau cho đến khi next_from = 0 (hết file) hoặc đã đủ nội dung cần trả lời. Mỗi trang có cột section — ghi căn cứ theo section đó khi trích dẫn [^n].`

// NewReadArticleTool bọc IVectorStore.ScrollRange + CountByFilter thành
// Eino InvokableTool — đọc từ Qdrant payload (DB chunk đã trim sau embed).
// Phạm vi tin cậy (user, category ép) đọc qua ScopeFrom(ctx).
func NewReadArticleTool(vectors domain.IVectorStore) (einotool.InvokableTool, error) {
	if vectors == nil {
		return nil, fmt.Errorf("eino read_article: vector store is required")
	}
	return toolutils.InferTool(ReadArticleName, readArticleDesc,
		func(ctx context.Context, in ReadArticleArgs) (*readArticlePage, error) {
			scope, ok := ScopeFrom(ctx)
			if !ok {
				return nil, fmt.Errorf("eino read_article: thiếu scope request (user_id) trong ctx")
			}
			if in.ArticleID <= 0 {
				// LLM gửi "None"/tên file thay vì số (model free) — trả trang
				// rỗng với next_from 0 để tự sửa ID, không abort run thành 500.
				return &readArticlePage{NextFrom: 0}, nil
			}
			from := int(in.From)
			if from < 0 {
				from = 0
			}
			limit := int(in.Limit)
			if limit <= 0 {
				limit = readArticleDefaultLimit
			}
			filter := domain.SearchFilter{
				UserID:      scope.UserID,
				CategoryIDs: scope.CategoryIDs,
				ArticleID:   int64(in.ArticleID),
			}
			chunks, err := vectors.ScrollRange(ctx, filter, from, limit)
			if err != nil {
				return nil, err
			}
			total, err := vectors.CountByFilter(ctx, filter)
			if err != nil {
				// Tổng số là phụ (hết trang khi next_from vượt) — không chặn
				// đọc vì lỗi đếm. Đếm PHẢI cùng filter scope với ScrollRange,
				// nếu không bài ngoài phạm vi sẽ lộ total_chunk.
				total = 0
			}
			if scope.Collector != nil {
				scope.Collector.MarkSearched()
			}
			page := &readArticlePage{
				ArticleID:  int64(in.ArticleID),
				From:       from,
				TotalChunk: total,
			}
			if len(chunks) == 0 {
				// ID không có chunk (bịa/chưa embed) — trang rỗng để LLM tự
				// xoay thay vì error abort cả run.
				return page, nil
			}
			// Sort tại tool: next_from = chunk cuối + 1 chỉ đúng khi trang
			// xếp theo ChunkIndex, không tin thứ tự store trả về.
			sort.Slice(chunks, func(i, j int) bool { return chunks[i].ChunkIndex < chunks[j].ChunkIndex })
			var ns []int
			if scope.Collector != nil {
				ns = scope.Collector.Add(chunks)
			} else {
				ns = make([]int, len(chunks))
				for i := range chunks {
					ns[i] = i + 1
				}
			}
			seen := map[string]bool{}
			for i, h := range chunks {
				if h == nil || ns[i] <= 0 {
					continue
				}
				if h.Section != "" && !seen[h.Section] {
					seen[h.Section] = true
					if page.Sections != "" {
						page.Sections += " | "
					}
					page.Sections += h.Section
				}
				page.Items = append(page.Items, struct {
					N          int    `json:"n"`
					ChunkIndex int    `json:"chunk_index"`
					PageNum    int    `json:"page_num"`
					Section    string `json:"section,omitempty"`
					Text       string `json:"text"`
				}{
					N: ns[i], ChunkIndex: h.ChunkIndex, PageNum: h.PageNum,
					Section: h.Section, Text: h.Text,
				})
			}
			// next_from = chunk cuối + 1; tổng không biết thì cứ dừng khi trang
			// ngắn hơn limit (hết chunk). Cả hai điều kiện thì an toàn.
			page.NextFrom = chunks[len(chunks)-1].ChunkIndex + 1
			if int64(page.NextFrom) >= total || len(chunks) < limit {
				page.NextFrom = 0
			}
			return page, nil
		})
}
