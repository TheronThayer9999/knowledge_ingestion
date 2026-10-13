package eino

import (
	"context"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"

	"knowledge_ingestion/src/domain"
)

// ListSectionsName là tên tool duy nhất LLM thấy.
const ListSectionsName = "list_sections"

// ListSectionsArgs là args LLM được phép đưa — article_id lấy từ hit
// knowledge_search hoặc article_info đã có, không bịa.
type ListSectionsArgs struct {
	ArticleID flexInt64 `json:"article_id" jsonschema:"description=ID bài viết lấy từ kết quả knowledge_search hoặc article_info. Bắt buộc."`
}

// listSectionsResult là danh sách mục của 1 bài — section_count là con số
// đáng tin để trả lời "có bao nhiêu Điều"; mỗi mục kèm from (chunk bắt đầu)
// làm tọa độ cho read_article nhảy đọc đúng vùng.
type listSectionsResult struct {
	ArticleID    int64          `json:"article_id,omitempty"`
	SectionCount int            `json:"section_count"`
	Sections     []sectionEntry `json:"sections"`
	// Note là lời nhắn cho LLM khi args không hợp lệ (id không phải số...).
	Note string `json:"note,omitempty"`
}

// sectionEntry là 1 mục + tọa độ chunk bắt đầu của nó.
type sectionEntry struct {
	Name string `json:"name"`
	From int    `json:"from"`
}

const listSectionsDesc = `Liệt kê các mục (Điều, chương, phần) của MỘT tài liệu đã upload, kèm số lượng mục và vị trí (from = chunk bắt đầu mỗi mục).

KHI NÀO GỌI: hỏi "có bao nhiêu Điều", "liệt kê các Điều", "file này gồm những phần nào", hoặc so sánh số lượng mục giữa các file ("file nào nhiều Điều hơn"). section_count là con số đáng tin — dùng nó để trả lời và so sánh, không tự đếm bằng mắt trên các đoạn trích, không so số hiệu lớn nhất (Điều 18 > Điều 10 về số hiệu không có nghĩa nhiều Điều hơn).

KHI NÀO KHÔNG GỌI: hỏi nội dung bên trong Điều (gọi knowledge_search); hỏi thông tin file như số trang, phiên bản (gọi article_info); chào hỏi, cảm ơn.

CÁCH DÙNG: lấy article_id từ kết quả knowledge_search hoặc article_info trước. So sánh 2 file thì gọi 1 lần cho mỗi file rồi so 2 con số section_count. Muốn đọc nội dung 1 mục cụ thể thì truyền from = chunk bắt đầu của mục đó (hoặc ít hơn 1-2 trang) vào read_article — khỏi cuộn từ đầu file.`

// NewListSectionsTool bọc IVectorStore.ListSections thành Eino InvokableTool
// — danh sách mục đọc từ Qdrant payload (DB chunk đã trim sau embed nên DB
// không còn đủ). Phạm vi tin cậy (user, category ép) đọc qua ScopeFrom(ctx).
func NewListSectionsTool(vectors domain.IVectorStore) (einotool.InvokableTool, error) {
	if vectors == nil {
		return nil, fmt.Errorf("eino list_sections: vector store is required")
	}
	return toolutils.InferTool(ListSectionsName, listSectionsDesc,
		func(ctx context.Context, in ListSectionsArgs) (*listSectionsResult, error) {
			scope, ok := ScopeFrom(ctx)
			if !ok {
				return nil, fmt.Errorf("eino list_sections: thiếu scope request (user_id) trong ctx")
			}
			if in.ArticleID <= 0 {
				// LLM gửi "None"/tên file thay vì số (model free) — trả count 0
				// với note để tự sửa ID, không abort run thành 500.
				return &listSectionsResult{
					Note: "article_id không hợp lệ — cần ID số lấy từ knowledge_search hoặc article_info",
				}, nil
			}
			sections, err := vectors.ListSections(ctx, domain.SearchFilter{
				UserID:      scope.UserID,
				CategoryIDs: scope.CategoryIDs,
				ArticleID:   int64(in.ArticleID),
			})
			if err != nil {
				return nil, err
			}
			if scope.Collector != nil {
				scope.Collector.MarkSearched()
			}
			if sections == nil {
				sections = []domain.SectionSpan{}
			}
			entries := make([]sectionEntry, 0, len(sections))
			for _, s := range sections {
				entries = append(entries, sectionEntry{Name: s.Name, From: s.From})
			}
			return &listSectionsResult{
				ArticleID:    int64(in.ArticleID),
				SectionCount: len(entries),
				Sections:     entries,
			}, nil
		})
}
