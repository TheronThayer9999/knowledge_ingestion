package eino

import (
	"context"
	stderrors "errors"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"

	"knowledge_ingestion/src/domain"
)

// ListUnreadablePagesName là tên tool duy nhất LLM thấy.
const ListUnreadablePagesName = "list_unreadable_pages"

const (
	// unreadableDefaultMinChars: dưới 40 ký tự coi là không đọc được. Đủ rộng
	// để bắt trang scan mờ (vài ký tự rác) và trang chỉ có số trang, đủ hẹp để
	// không oan một khoản/đoạn ngắn thật.
	unreadableDefaultMinChars = 40
	// unreadableMaxMinChars chặn LLM truyền ngưỡng vô lý (1000) làm mọi trang
	// đều "lỗi", đầu độc câu trả lời.
	unreadableMaxMinChars = 1000
	// unreadableMaxIssues chặn context nổ khi cả file scan hỏng (500 trang):
	// chỉ liệt kê chừng này trang đầu, con số tổng vẫn đúng.
	unreadableMaxIssues = 50
)

// ListUnreadablePagesArgs là args LLM được phép đưa — article_id lấy từ
// knowledge_search/article_info, min_chars chỉ để chỉnh ngưỡng khi cần.
type ListUnreadablePagesArgs struct {
	ArticleID flexInt64 `json:"article_id" jsonschema:"description=ID bài viết cần kiểm tra (lấy từ knowledge_search hoặc article_info). Bắt buộc."`
	MinChars  flexInt   `json:"min_chars,omitempty" jsonschema:"description=Ngưỡng coi là không đọc được, tính theo ký tự (bỏ trống = 40, tối đa 1000)."`
}

// unreadablePage là 1 trang mất nội dung — page_num là số trang THẬT để agent
// trích dẫn, seq là thứ tự đọc cho file không phân trang (txt/docx).
type unreadablePage struct {
	PageNum     int     `json:"page_num"`
	Seq         int     `json:"seq"`
	Chars       int     `json:"chars"`
	LayoutScore float64 `json:"layout_score,omitempty"`
}

// unreadablePagesResult là báo cáo độ phủ của 1 file: issue_count > 0 nghĩa
// là chỉ mục text THIẾU nội dung ở các trang đó.
type unreadablePagesResult struct {
	ArticleID  int64            `json:"article_id,omitempty"`
	PageCount  int              `json:"page_count"`
	MinChars   int              `json:"min_chars"`
	IssueCount int              `json:"issue_count"`
	Issues     []unreadablePage `json:"issues"`
	// Note là lời nhắn cho LLM: args sai, bài chưa trích, hoặc cảnh báo phải
	// nói thật với người dùng là tài liệu thiếu phần.
	Note string `json:"note,omitempty"`
}

const listUnreadablePagesDesc = `Kiểm tra MỘT tài liệu đã upload xem có trang nào bị MẤT NỘI DUNG khi trích (scan mờ, trang trắng, trang chỉ có vài ký tự) — trả về số trang thật của những trang đó.

KHI NÀO GỌI: người dùng hỏi tài liệu có thiếu trang/thiếu nội dung không, "file scan này có đọc được hết không", hoặc khi knowledge_search/read_article cho thấy nội dung hụt, nhảy cóc giữa các Điều mà không rõ lý do.

KHI NÀO KHÔNG GỌI: hỏi nội dung bình thường (knowledge_search, read_article); hỏi số trang/phiên bản (article_info); đếm số Điều (list_sections); chào hỏi.

CÁCH DÙNG: truyền article_id lấy từ knowledge_search hoặc article_info. issue_count = 0 là tài liệu đã trích ĐỦ, không cần nhắc gì thêm. issue_count > 0 thì đọc note: nội dung các trang này KHÔNG có trong chỉ mục — phải nói rõ với người dùng là tài liệu thiếu phần ở các trang đó, TUYỆT ĐỐI không suy đoán nội dung.`

// NewListUnreadablePagesTool bọc page repo thành Eino InvokableTool — chỉ đọc
// trang của bài mình sở hữu (GetByID khóa user_id) và trong category scope
// của request, LLM không có đường soi trang của bài user khác.
//
// VÌ SAO TOOL NÀY TỒN TẠI: extract chỉ fail khi CẢ file không ra chữ; file
// 100 trang mà 3 trang scan mờ vẫn "done" và 3 trang đó thành text rỗng trong
// article_pages — không ai biết. Đây là kiểu sai im lặng: người dùng hỏi đúng
// phần ở trang 3 sẽ nhận câu trả lời thiếu mà không có cảnh báo.
func NewListUnreadablePagesTool(articles domain.IArticleRepository, pages domain.ArticlePageIssues) (einotool.InvokableTool, error) {
	if articles == nil || pages == nil {
		return nil, fmt.Errorf("eino list_unreadable_pages: article repo và page repo đều bắt buộc")
	}
	return toolutils.InferTool(ListUnreadablePagesName, listUnreadablePagesDesc,
		func(ctx context.Context, in ListUnreadablePagesArgs) (*unreadablePagesResult, error) {
			scope, ok := ScopeFrom(ctx)
			if !ok {
				return nil, fmt.Errorf("eino list_unreadable_pages: thiếu scope request (user_id) trong ctx")
			}
			if in.ArticleID <= 0 {
				// LLM gửi "None"/tên file thay vì số (model free) — trả note
				// để tự sửa ID, không abort run thành 500.
				return &unreadablePagesResult{
					MinChars: unreadableDefaultMinChars,
					Issues:   []unreadablePage{},
					Note:     "article_id không hợp lệ — cần ID số lấy từ knowledge_search hoặc article_info",
				}, nil
			}
			a, err := articles.GetByID(ctx, int64(in.ArticleID), scope.UserID)
			// Bài không tồn tại (LLM bịa ID/đã xoá) — trả rỗng để LLM tự xoay;
			// lỗi hạ tầng (DB) vẫn trả error để caller map 500 đúng.
			if stderrors.Is(err, domain.ErrNotFound) || (err == nil && a == nil) {
				return &unreadablePagesResult{
					ArticleID: int64(in.ArticleID),
					MinChars:  unreadableDefaultMinChars,
					Issues:    []unreadablePage{},
					Note:      "không tìm thấy tài liệu trong phạm vi của bạn",
				}, nil
			}
			if err != nil {
				return nil, err
			}
			// Request có thể đã khoanh thêm category — bài ngoài phạm vi trả
			// rỗng thay vì lộ là bài đó có bao nhiêu trang hỏng.
			if !scope.AllowsCategory(a.CategoryID) {
				return &unreadablePagesResult{
					ArticleID: a.ID,
					MinChars:  unreadableDefaultMinChars,
					Issues:    []unreadablePage{},
					Note:      "không tìm thấy tài liệu trong phạm vi của bạn",
				}, nil
			}
			minChars := int(in.MinChars)
			if minChars <= 0 {
				minChars = unreadableDefaultMinChars
			}
			if minChars > unreadableMaxMinChars {
				minChars = unreadableMaxMinChars
			}
			found, err := pages.ListPageIssues(ctx, a.ID, minChars)
			if err != nil {
				return nil, err
			}
			if scope.Collector != nil {
				scope.Collector.MarkSearched()
			}
			res := &unreadablePagesResult{
				ArticleID:  a.ID,
				PageCount:  a.PageCount,
				MinChars:   minChars,
				IssueCount: len(found),
				Issues:     make([]unreadablePage, 0, len(found)),
			}
			for _, p := range found {
				if len(res.Issues) >= unreadableMaxIssues {
					break
				}
				res.Issues = append(res.Issues, unreadablePage{
					PageNum: p.PageNum, Seq: p.Seq, Chars: p.Chars, LayoutScore: p.LayoutScore,
				})
			}
			res.Note = unreadableNote(res, len(found) > unreadableMaxIssues)
			return res, nil
		})
}

// unreadableNote là câu chỉ dẫn cho LLM theo tình huống — tool không tự quyết
// định cách trả lời, chỉ nói sự thật về dữ liệu.
func unreadableNote(res *unreadablePagesResult, truncated bool) string {
	if res.IssueCount == 0 {
		// Chưa trích xong thì KHÔNG được coi là sạch: không có trang nào để
		// kiểm là chưa có dữ liệu, khác với "đã trích đủ".
		if res.PageCount == 0 {
			return "tài liệu chưa trích xong (chưa có số trang) — chưa kiểm tra được trang nào lỗi, thử lại sau"
		}
		return ""
	}
	var note string
	if res.PageCount > 0 {
		note = fmt.Sprintf("có %d/%d trang thiếu nội dung trong chỉ mục (scan mờ/trang trắng/chỉ vài ký tự)", res.IssueCount, res.PageCount)
	} else {
		// txt/docx chưa có số trang: báo theo thứ tự đọc.
		note = fmt.Sprintf("có %d mảnh thiếu nội dung trong chỉ mục (scan mờ/trang trắng/chỉ vài ký tự)", res.IssueCount)
	}
	if truncated {
		note += fmt.Sprintf("; chỉ liệt kê %d trang đầu", unreadableMaxIssues)
	}
	return note + ". Nếu câu hỏi chạm vào các trang này, nói rõ tài liệu thiếu phần đó — KHÔNG suy đoán nội dung."
}
