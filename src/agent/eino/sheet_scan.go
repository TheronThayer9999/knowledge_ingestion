package eino

import (
	"context"
	stderrors "errors"
	"strings"

	"knowledge_ingestion/src/common/extractor"
	"knowledge_ingestion/src/domain"
)

// Phần dùng chung của 2 tool sheet (list_sheets/read_sheet): đọc text đã trích
// rồi tách thành khối sheet, và kiểm tra quyền sở hữu bài một lần.

const (
	// sheetScanWindow số mảnh mỗi lượt đọc — cùng cửa sổ nhỏ với phase index,
	// đủ để một lần đọc đã thấy nhiều sheet mà vẫn không kéo cả file lên RAM.
	sheetScanWindow = 20
	// sheetScanMaxChars trần text quét cho 1 lần gọi tool. Excel thật hiếm khi
	// vượt (4MB text ≈ hàng chục nghìn hàng); chạm trần thì báo truncated
	// thay vì treo request chat vì file khổng lồ.
	sheetScanMaxChars = 4 << 20
	// sheetMarker là dấu hiệu duy nhất cho biết text này của file Excel —
	// extractXlsx luôn mở đầu bằng dòng tiêu đề sheet đầu tiên.
	sheetMarker = "## Sheet"
)

// sheetState nói vì sao không có sheet để trả — tool phải nói thật với LLM
// từng trường hợp, không được gộp "chưa trích" với "không có sheet".
type sheetState int

const (
	// sheetStateOK: text có dòng tiêu đề sheet, đã parse được.
	sheetStateOK sheetState = iota
	// sheetStateNotExtracted: bài chưa có mảnh nào (worker chưa trích xong).
	sheetStateNotExtracted
	// sheetStateNotExcel: đã trích nhưng không có dấu hiệu sheet nào.
	sheetStateNotExcel
)

// articleSheets đọc text đã trích của 1 bài theo cửa sổ rồi tách thành khối
// sheet. Dừng ngay ở mảnh đầu nếu file không phải Excel — nhờ vậy gọi nhầm
// tool lên PDF 5000 trang chỉ tốn 1 câu SELECT thay vì quét cả file.
//
// Điều kiện dừng vòng đọc là len(batch) == 0, KHÔNG phải "lượt đọc ngắn hơn
// window" — cùng lý do với phase index: repo trả đủ trang dù ngắn hơn limit.
func articleSheets(ctx context.Context, pages domain.ArticlePageText, articleID int64) ([]extractor.SheetBlock, bool, sheetState, error) {
	first, err := pages.ListPagesRange(ctx, articleID, 1, 1)
	if err != nil {
		return nil, false, sheetStateNotExtracted, err
	}
	if len(first) == 0 {
		return nil, false, sheetStateNotExtracted, nil
	}
	if !strings.Contains(first[0].Text, sheetMarker) {
		return nil, false, sheetStateNotExcel, nil
	}

	var (
		buf       strings.Builder
		truncated bool
	)
	for from := 1; ; {
		batch, err := pages.ListPagesRange(ctx, articleID, from, sheetScanWindow)
		if err != nil {
			return nil, false, sheetStateOK, err
		}
		if len(batch) == 0 {
			break
		}
		for _, p := range batch {
			if buf.Len()+len(p.Text) > sheetScanMaxChars {
				truncated = true
				break
			}
			buf.WriteString(p.Text)
			// Mảnh luôn cắt ở ranh giới dòng (trừ mảnh cuối) nên chỉ thêm
			// newline khi mảnh không tự kết thúc bằng nó — không tạo dòng rác
			// làm sai số hàng.
			if !strings.HasSuffix(p.Text, "\n") {
				buf.WriteString("\n")
			}
			from = p.Seq + 1
		}
		if truncated {
			break
		}
	}
	return extractor.ParseSheetBlocks(buf.String()), truncated, sheetStateOK, nil
}

// ownedArticle kiểm tra scope request + quyền sở hữu bài cho tool đọc nội dung.
// note khác rỗng nghĩa là "không dùng được" (LLM tự sửa args), err chỉ dành
// cho lỗi hạ tầng — args sai không được làm sập cả run thành 500.
func ownedArticle(ctx context.Context, scope Scope, articles domain.IArticleRepository, id flexInt64) (*domain.Article, string, error) {
	if id <= 0 {
		return nil, "article_id không hợp lệ — cần ID số lấy từ knowledge_search hoặc article_info", nil
	}
	a, err := articles.GetByID(ctx, int64(id), scope.UserID)
	// Bài không tồn tại (LLM bịa ID/đã xoá) — trả note để LLM tự xoay.
	if stderrors.Is(err, domain.ErrNotFound) || (err == nil && a == nil) {
		return nil, "không tìm thấy tài liệu trong phạm vi của bạn", nil
	}
	if err != nil {
		return nil, "", err
	}
	// Request có thể đã khoanh thêm category — bài ngoài phạm vi trả cùng câu
	// với "không tìm thấy" để không lộ là bài đó tồn tại.
	if !scope.AllowsCategory(a.CategoryID) {
		return nil, "không tìm thấy tài liệu trong phạm vi của bạn", nil
	}
	return a, "", nil
}
