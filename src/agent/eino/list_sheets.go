package eino

import (
	"context"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"

	"knowledge_ingestion/src/domain"
)

// ListSheetsName là tên tool duy nhất LLM thấy.
const ListSheetsName = "list_sheets"

// listSheetsMaxNames giới hạn số tên sheet ghi vào note khi LLM chọn nhầm —
// file có 40 sheet thì note vẫn phải đọc được, không phình context.
const listSheetsMaxNames = 12

// ListSheetsArgs là args LLM được phép đưa — article_id lấy từ
// knowledge_search/article_info, không bịa.
type ListSheetsArgs struct {
	ArticleID flexInt64 `json:"article_id" jsonschema:"description=ID bài viết Excel cần liệt kê sheet (lấy từ knowledge_search hoặc article_info). Bắt buộc."`
}

// sheetEntry là 1 sheet trong danh sách: rows là số HÀNG DỮ LIỆU (không tính
// hàng header), columns là hàng đầu tiên đã tách ô.
type sheetEntry struct {
	Index   int      `json:"index"`
	Name    string   `json:"name"`
	Rows    int      `json:"rows"`
	Columns []string `json:"columns,omitempty"`
	Chars   int      `json:"chars"`
}

type listSheetsResult struct {
	ArticleID  int64        `json:"article_id,omitempty"`
	SheetCount int          `json:"sheet_count"`
	Sheets     []sheetEntry `json:"sheets"`
	// Note nói thật lý do khi không có sheet (chưa trích, không phải Excel,
	// file quá lớn) để LLM không bịa ra sheet.
	Note string `json:"note,omitempty"`
}

const listSheetsDesc = `Liệt kê các SHEET của MỘT file Excel (.xlsx) đã upload: tên sheet, số hàng dữ liệu, tên cột (hàng đầu tiên của sheet) và độ dài nội dung.

KHI NÀO GỌI: người dùng hỏi "file Excel này có những sheet nào", "bảng lương nằm ở sheet nào", "sheet nào có cột phụ cấp"; cần biết cấu trúc bảng trước khi đọc dữ liệu; so sánh cấu trúc 2 file Excel.

KHI NÀO KHÔNG GỌI: hỏi nội dung văn bản thường (knowledge_search, read_article); đếm Điều/mục (list_sections); hỏi số trang/phiên bản (article_info); chào hỏi. File không phải Excel thì tool trả sheet_count = 0 kèm note — khi đó đừng dùng tool này nữa.

CÁCH DÙNG: article_id lấy từ knowledge_search hoặc article_info. sheet_count là số sheet THẬT trong file — dùng nó để trả lời, đừng đếm bằng cách đọc nội dung. columns là hàng đầu tiên của sheet; nếu file không có header thì hàng đó vẫn là dữ liệu, đừng tự ý bỏ qua. Muốn đọc dữ liệu thì gọi read_sheet với index hoặc name của sheet vừa liệt kê.`

// NewListSheetsTool bọc page repo (text đã trích) thành Eino InvokableTool.
//
// VÌ SAO TOOL NÀY TỒN TẠI: extractXlsx dồn cả workbook thành một dòng chảy
// "## Sheet: tên" + các dòng pipe. knowledge_search/read_article trả về các
// đoạn rời, không cho biết file có mấy sheet, sheet nào chứa gì — agent trả lời
// "bảng lương ở đâu" bằng cách đoán từ vài đoạn trích là kiểu sai im lặng.
// Đọc sheet theo yêu cầu thì câu trả lời bám đúng cấu trúc file.
func NewListSheetsTool(articles domain.IArticleRepository, pages domain.ArticlePageText) (einotool.InvokableTool, error) {
	if articles == nil || pages == nil {
		return nil, fmt.Errorf("eino list_sheets: article repo và page repo đều bắt buộc")
	}
	return toolutils.InferTool(ListSheetsName, listSheetsDesc,
		func(ctx context.Context, in ListSheetsArgs) (*listSheetsResult, error) {
			scope, ok := ScopeFrom(ctx)
			if !ok {
				return nil, fmt.Errorf("eino list_sheets: thiếu scope request (user_id) trong ctx")
			}
			res := &listSheetsResult{Sheets: []sheetEntry{}}
			a, note, err := ownedArticle(ctx, scope, articles, in.ArticleID)
			if err != nil {
				return nil, err
			}
			if a == nil {
				res.Note = note
				return res, nil
			}
			res.ArticleID = a.ID
			blocks, truncated, state, err := articleSheets(ctx, pages, a.ID)
			if err != nil {
				return nil, err
			}
			if scope.Collector != nil {
				scope.Collector.MarkSearched()
			}
			switch state {
			case sheetStateNotExtracted:
				res.Note = "tài liệu chưa trích xong nên chưa biết có sheet nào — thử lại sau"
				return res, nil
			case sheetStateNotExcel:
				res.Note = "tài liệu này không có sheet (chỉ file Excel .xlsx mới có) — dùng list_sections/read_article để đọc nội dung"
				return res, nil
			}
			res.SheetCount = len(blocks)
			for _, b := range blocks {
				res.Sheets = append(res.Sheets, sheetEntry{
					Index: b.Index, Name: b.Name, Rows: b.TotalRows(),
					Columns: b.Columns, Chars: b.Chars,
				})
			}
			res.Note = listSheetsNote(res, truncated)
			return res, nil
		})
}

// listSheetsNote chỉ nói sự thật về dữ liệu: file lớn quét chưa hết, hoặc file
// Excel không có sheet nào có nội dung — không tự quyết định cách trả lời.
func listSheetsNote(res *listSheetsResult, truncated bool) string {
	var note string
	switch res.SheetCount {
	case 0:
		note = "file Excel không có sheet nào có nội dung"
	default:
		empty := 0
		for _, s := range res.Sheets {
			if s.Rows == 0 {
				empty++
			}
		}
		if empty > 0 {
			note = fmt.Sprintf("%d sheet chỉ có header, không có hàng dữ liệu", empty)
		}
	}
	if truncated {
		if note != "" {
			note += "; "
		}
		note += "file quá lớn nên chỉ quét được phần đầu — số sheet có thể còn thiếu"
	}
	return note
}
