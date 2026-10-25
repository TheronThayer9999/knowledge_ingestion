package eino

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"

	"knowledge_ingestion/src/common/extractor"
	"knowledge_ingestion/src/domain"
)

// ReadSheetName là tên tool duy nhất LLM thấy.
const ReadSheetName = "read_sheet"

const (
	// readSheetDefaultRows số hàng mỗi lần đọc khi LLM không truyền — đủ thấy
	// cấu trúc + vài chục dòng dữ liệu mà không phình context.
	readSheetDefaultRows = 60
	// readSheetMaxRows trần hàng/lần gọi: chặn "cho tôi cả sheet" biến câu trả
	// lời thành dump 20k dòng, vượt context model.
	readSheetMaxRows = 300
)

// ReadSheetArgs là args LLM được phép đưa. Sheet nhận cả số thứ tự ("2") lẫn
// tên ("Bảng lương") vì LLM đọc từ kết quả list_sheets theo kiểu nào cũng có.
type ReadSheetArgs struct {
	ArticleID flexInt64 `json:"article_id" jsonschema:"description=ID bài viết Excel (lấy từ knowledge_search, article_info hoặc list_sheets). Bắt buộc."`
	Sheet     string    `json:"sheet,omitempty" jsonschema:"description=Số thứ tự ('2') hoặc tên sheet ('Bảng lương') lấy từ list_sheets. Bỏ trống khi file chỉ có 1 sheet."`
	FromRow   flexInt   `json:"from_row,omitempty" jsonschema:"description=Hàng dữ liệu bắt đầu, tính từ 1 và KHÔNG tính hàng header (bỏ trống = 1). Trang sau truyền next_from_row của lần trước."`
	MaxRows   flexInt   `json:"max_rows,omitempty" jsonschema:"description=Số hàng mỗi lần đọc (mặc định 60, tối đa 300)."`
}

// readSheetResult là 1 trang dữ liệu của sheet: text gồm header + các hàng được
// chọn, next_from_row = 0 nghĩa là đã hết sheet.
type readSheetResult struct {
	ArticleID   int64    `json:"article_id,omitempty"`
	Sheet       string   `json:"sheet,omitempty"`
	Index       int      `json:"index,omitempty"`
	Columns     []string `json:"columns,omitempty"`
	TotalRows   int      `json:"total_rows"`
	FromRow     int      `json:"from_row,omitempty"`
	ToRow       int      `json:"to_row,omitempty"`
	NextFromRow int      `json:"next_from_row,omitempty"`
	Text        string   `json:"text,omitempty"`
	Note        string   `json:"note,omitempty"`
}

const readSheetDesc = `Đọc dữ liệu MỘT sheet của file Excel (.xlsx) theo HÀNG, có phân trang: trả về các dòng bảng pipe (đúng vị trí cột) kèm tổng số hàng của sheet.

KHI NÀO GỌI: người dùng hỏi nội dung bên trong một bảng Excel ("sheet Bảng lương có những dòng nào", "liệt kê các mức phí trong sheet Phí"), hoặc cần đối chiếu từng hàng; sau khi list_sheets cho biết file có sheet nào.

KHI NÀO KHÔNG GỌI: chưa biết file có sheet nào (gọi list_sheets trước); hỏi văn bản thường (knowledge_search, read_article); file không phải Excel.

CÁCH DÙNG: gọi list_sheets trước rồi truyền sheet (số thứ tự hoặc tên). Dòng đầu của text là header, các dòng sau là dữ liệu bắt đầu từ from_row. total_rows là số hàng dữ liệu thật của sheet; next_from_row > 0 thì còn hàng — gọi lại với from_row = next_from_row, đừng suy đoán phần chưa đọc.`

// NewReadSheetTool bọc page repo thành Eino InvokableTool — cùng nguồn
// article_pages với list_sheets, nên số hàng hai tool báo luôn khớp nhau.
func NewReadSheetTool(articles domain.IArticleRepository, pages domain.ArticlePageText) (einotool.InvokableTool, error) {
	if articles == nil || pages == nil {
		return nil, fmt.Errorf("eino read_sheet: article repo và page repo đều bắt buộc")
	}
	return toolutils.InferTool(ReadSheetName, readSheetDesc,
		func(ctx context.Context, in ReadSheetArgs) (*readSheetResult, error) {
			scope, ok := ScopeFrom(ctx)
			if !ok {
				return nil, fmt.Errorf("eino read_sheet: thiếu scope request (user_id) trong ctx")
			}
			res := &readSheetResult{}
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
				res.Note = "tài liệu chưa trích xong nên chưa có sheet để đọc — thử lại sau"
				return res, nil
			case sheetStateNotExcel:
				res.Note = "tài liệu này không có sheet (chỉ file Excel .xlsx mới có) — dùng read_article để đọc nội dung"
				return res, nil
			}
			if len(blocks) == 0 {
				res.Note = "file Excel không có sheet nào có nội dung"
				return res, nil
			}
			block, note := pickSheet(blocks, in.Sheet)
			if note != "" {
				res.Note = note
				return res, nil
			}
			res.Sheet, res.Index, res.Columns = block.Name, block.Index, block.Columns
			res.TotalRows = block.TotalRows()

			from := int(in.FromRow)
			if from < 1 {
				from = 1
			}
			limit := int(in.MaxRows)
			if limit <= 0 {
				limit = readSheetDefaultRows
			}
			if limit > readSheetMaxRows {
				limit = readSheetMaxRows
			}
			if from > block.TotalRows() {
				// from_row vượt quá là args sai (thường do LLM tự cộng), không
				// phải lỗi — trả header + số hàng thật để tự sửa.
				res.Text = block.Header
				res.Note = fmt.Sprintf("sheet chỉ có %d hàng dữ liệu — from_row %d vượt quá, đọc lại từ from_row=1", block.TotalRows(), from)
				return res, nil
			}
			to := from + limit - 1
			if to > block.TotalRows() {
				to = block.TotalRows()
			}
			lines := make([]string, 0, to-from+2)
			if block.Header != "" {
				lines = append(lines, block.Header)
			}
			lines = append(lines, block.Rows[from-1:to]...)
			res.Text = strings.Join(lines, "\n")
			res.FromRow, res.ToRow = from, to
			if to < block.TotalRows() {
				res.NextFromRow = to + 1
			}
			res.Note = truncatedNote(res, truncated)
			return res, nil
		})
}

// truncatedNote nhắc LLM đọc tiếp thay vì kết luận "sheet chỉ có chừng này".
func truncatedNote(res *readSheetResult, truncated bool) string {
	var note string
	if res.NextFromRow > 0 {
		note = fmt.Sprintf("còn %d hàng — gọi lại với from_row=%d", res.TotalRows-res.ToRow, res.NextFromRow)
	}
	if truncated {
		if note != "" {
			note += "; "
		}
		note += "file quá lớn nên chỉ quét được phần đầu — số hàng có thể còn thiếu"
	}
	return note
}

// pickSheet chọn sheet theo số thứ tự hoặc tên. Trả note khác rỗng khi không
// chọn được: không có sheet đó, tên khớp nhiều sheet, hoặc file nhiều sheet mà
// LLM không chỉ định. Không trả error vì args sai không phải lỗi hạ tầng.
func pickSheet(blocks []extractor.SheetBlock, sel string) (extractor.SheetBlock, string) {
	sel = strings.TrimSpace(sel)
	if n, err := strconv.Atoi(sel); err == nil {
		if n < 1 || n > len(blocks) {
			return extractor.SheetBlock{}, fmt.Sprintf("không có sheet số %d — file có %d sheet: %s", n, len(blocks), sheetNames(blocks))
		}
		return blocks[n-1], ""
	}
	if sel == "" {
		if len(blocks) == 1 {
			return blocks[0], ""
		}
		return extractor.SheetBlock{}, fmt.Sprintf("cần chỉ định sheet — file có %d sheet: %s", len(blocks), sheetNames(blocks))
	}
	// Khớp chính xác trước: LLM thường chép nguyên tên từ list_sheets.
	for _, b := range blocks {
		if strings.EqualFold(strings.TrimSpace(b.Name), sel) {
			return b, ""
		}
	}
	var hits []extractor.SheetBlock
	for _, b := range blocks {
		if strings.Contains(strings.ToLower(b.Name), strings.ToLower(sel)) {
			hits = append(hits, b)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], ""
	case 0:
		return extractor.SheetBlock{}, fmt.Sprintf("không có sheet tên %q — file có: %s", sel, sheetNames(blocks))
	default:
		return extractor.SheetBlock{}, fmt.Sprintf("%q khớp %d sheet: %s — chọn theo số thứ tự", sel, len(hits), sheetNames(hits))
	}
}

// sheetNames liệt kê "1=tên, 2=tên" cho note, cắt ở listSheetsMaxNames để file
// nhiều sheet không làm note dài vô hạn.
func sheetNames(blocks []extractor.SheetBlock) string {
	var b strings.Builder
	for i, s := range blocks {
		if i == listSheetsMaxNames {
			b.WriteString(", ...")
			break
		}
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%d=%s", s.Index, s.Name)
	}
	return b.String()
}
