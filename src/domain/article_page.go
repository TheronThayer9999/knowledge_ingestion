package domain

import (
	"context"
)

// ArticlePage là TEXT THÔ của 1 trang — nguồn sự thật của nội dung bài sau
// khi phase extract xong.
//
// VÌ SAO GIỮ LẠI THAY VÌ GỘP THÀNH 1 CỘT TEXT: phase index đọc theo CỬA SỔ
// TRƯỢT vài trang một (xem IArticlePageRepository.ListPagesRange) nên RAM
// không phụ thuộc kích thước file. Gộp cả file vào 1 cột articles thì mỗi
// lần index phải kéo toàn bộ text lên RAM — file 500 trang scan là vài chục
// MB rác phải sống suốt job.
//
// VÌ SAO CÓ BẢNG NÀY: OCR trang scan mất ~2s, file 500 trang là ~17 phút.
// Worker extract ghi mỗi EXTRACT_FLUSH_PAGES trang 1 lô rồi mới đi tiếp, nên
// job chết/hết giờ chỉ mất tối đa 1 lô chứ không mất trắng cả file.
//
// Text ở đây là BẢN THÔ (chưa khử chữ rải, chưa bỏ dòng lặp) vì quyết định
// "cả file này thế nào" chỉ chốt được khi đã quét hết file — nó được tính ở
// phase index bằng 1 lượt quét trước (extractor.Cleaner), nên giữ thô ở đây
// vừa tiết kiệm ghi 2 lần vừa không sinh trạng thái nửa vời (một trang đã
// sạch, một trang chưa).
type ArticlePage struct {
	BaseModel

	// Seq là THỨ TỰ ĐỌC của mảnh trong bài, tăng liên tục từ 1. Cùng
	// (article_id, seq) là khoá tự nhiên, và nó — KHÔNG phải PageNum — là
	// con trỏ "đọc tới đâu" của phase index.
	//
	// VÌ SAO TÁCH KHỎI PageNum: file txt/md không có khái niệm trang nên mọi
	// mảnh đều PageNum = 0. Trước đây unique (article_id, page_num) buộc
	// cả file txt phải là 1 row — nên file 100MB hoặc phải cắt cụt mất
	// dữ liệu, hoặc kéo cả 100MB lên RAM 1 lần. Nay extractor chia txt thành
	// nhiều mảnh TextPieceBytes, mọi mảnh cùng PageNum 0 nhưng Seq khác nhau.
	//
	// PageNum vẫn giữ nguyên ý nghĩa cũ: số trang THẬT để trích dẫn (PDF),
	// 0 cho loại file không có trang.
	//
	// default:0 là BẮT BUỘC: AutoMigrate chạy trước migrateQueueColumns,
	// thêm cột NOT NULL vào bảng đã có dữ liệu sẽ fail với
	// "column seq contains null values". Có DEFAULT thì Postgres gán 0 cho
	// dòng cũ, migrate.go backfill lại giá trị thật sau.
	Seq int `json:"seq" gorm:"not null;default:0"`
	// ArticleID + Seq là khoá tự nhiên: 1 bài không có 2 mảnh cùng seq, và
	// 2 worker claim trùng (lease race) insert cùng mảnh thì ON CONFLICT
	// DO NOTHING giữ 1 bản — cùng pattern ON CONFLICT của các bảng khác.
	ArticleID int64 `json:"article_id" gorm:"not null"`
	PageNum   int   `json:"page_num" gorm:"not null"`
	// Text là text thô của mảnh, có thể rỗng (trang trắng, trang lỗi, PDF
	// không nhúng font) — vẫn giữ row để số trang khớp file gốc.
	Text string `json:"text" gorm:"type:text;not null"`
	// LayoutScore là ĐIỂM bố cục phức tạp của trang trong [0,1] do phase
	// extract đo (extractor.DetectLayout trên text `pdftotext -layout`).
	//
	// VÌ SAO LƯU ĐIỂM CHỨ KHÔNG LƯU BOOL: ngưỡng "bao nhiêu là bảng" cần
	// calibrate trên dữ liệu thật. Lưu điểm thì đổi ngưỡng chỉ là đổi hằng số
	// rồi chạy một câu SELECT/UPDATE dẫn xuất, không phải trích lại cả kho.
	// Câu hỏi "trang nào cần L2/VLM" = `WHERE layout_score >= ngưỡng`.
	//
	// 0 nghĩa là "trang sạch" HOẶC "lúc trích chưa bật đo" — đây là lý do
	// mặc định của tính năng là TẮT, và cũng là lý do chỉ số này KHÔNG được
	// dùng để kết luận sạch, chỉ để lọc ra trang đáng xem lại.
	//
	// default:0 BẮT BUỘC cùng lý do với Seq: AutoMigrate thêm cột NOT NULL
	// vào bảng đã có dữ liệu sẽ fail nếu không có DEFAULT.
	LayoutScore float64 `json:"layout_score" gorm:"not null;default:0"`
}

func (ArticlePage) TableName() string {
	return "article_pages"
}

// PageProgress là con trỏ tiến độ của phase extract — đọc 1 câu để biết
// "đã trích tới mảnh nào" sau khi job chết giữa đường.
type PageProgress struct {
	// DoneSeq là seq lớn nhất đã lưu, -1 khi bài chưa có mảnh nào. Kỳ sau
	// lấy seq kế tiếp làm fromPage để extractor bỏ qua hoàn toàn phần đã xong.
	DoneSeq int
}

// PageIssue là 1 trang coi như KHÔNG ĐỌC ĐƯỢC — text rỗng hoặc quá ngắn so
// với ngưỡng. Cố ý KHÔNG có field Text: mục đích là để agent biết "trang nào
// thiếu" (rồi nói thật với người dùng, hoặc đọc lại bằng ảnh), không phải để
// đọc nội dung — kéo text về đây chỉ tổ phình context mà toàn ký tự vô nghĩa.
type PageIssue struct {
	// PageNum là số trang THẬT (PDF); 0 với loại file không phân trang.
	PageNum int `json:"page_num"`
	// Seq là thứ tự đọc — trích dẫn khi PageNum = 0, và là khoá sắp xếp.
	Seq int `json:"seq"`
	// Chars là số KÝ TỰ của trang sau khi bỏ khoảng trắng: bằng chứng định
	// lượng cho LLM (0 = trang trắng/mất chữ hẳn, 12 = còn vài dòng sót).
	Chars int `json:"chars"`
	// LayoutScore là điểm bố cục đo lúc trích (0 = không đo) — phân biệt
	// "trang bảng bị mất chữ" với "trang trắng thật".
	LayoutScore float64 `json:"layout_score,omitempty"`
}

// ArticlePageIssues là VAI HẸP của page repo cho agent: chỉ hỏi "trang nào
// không đọc được", không đụng đường ghi của worker. Tách khỏi
// IArticlePageRepository để tool không kéo theo 6 method mà nó không dùng
// (ISP) và test chỉ cần 1 hàm giả.
type ArticlePageIssues interface {
	// ListPageIssues trả các trang có text ngắn hơn minChars ký tự, theo
	// thứ tự đọc. Trang không tồn tại thì trả rỗng, không lỗi.
	ListPageIssues(ctx context.Context, articleID int64, minChars int) ([]PageIssue, error)
}

// ArticlePageText là VAI HẸP "đọc text đã trích theo cửa sổ" cho tool agent
// (list_sheets/read_sheet) — chỉ ĐỌC, không ghi/không xoá như đường của
// worker, nên tool không phải cầm cả IArticlePageRepository (ISP) và test
// chỉ cần 1 hàm giả.
//
// VÌ SAO ĐỌC THEO CỬA SỔ CHỨ KHÔNG ListByArticle: cùng lý do với phase index
// — tài liệu lớn không được kéo hết text lên RAM chỉ để trả lời "có sheet
// nào"; tool tự dừng khi đủ hoặc chạm trần.
type ArticlePageText interface {
	// ListPagesRange trả tối đa limit mảnh có seq >= fromSeq, theo thứ tự seq.
	ListPagesRange(ctx context.Context, articleID int64, fromSeq, limit int) ([]*ArticlePage, error)
}

type IArticlePageRepository interface {
	// CreateBatch lưu 1 lô mảnh trong transaction, ON CONFLICT DO NOTHING
	// theo unique (article_id, seq) — lô ghi lại sau khi job chết là no-op
	// chứ không nhân bản.
	CreateBatch(ctx context.Context, pages []*ArticlePage) error
	// Progress đọc seq lớn nhất đã lưu trong 1 câu — phase extract gọi
	// 1 lần mỗi bài rồi bỏ qua phần đã xong.
	Progress(ctx context.Context, articleID int64) (*PageProgress, error)
	// ListByArticle trả mọi mảnh theo thứ tự đọc — dùng khi cần xử lý toàn
	// bộ (hiếm; worker chủ yếu đọc theo cửa sổ).
	ListByArticle(ctx context.Context, articleID int64) ([]*ArticlePage, error)
	// ListPagesRange trả tối đa limit mảnh có seq >= fromSeq, theo thứ tự
	// seq — CỬA SỔ TRƯỢT của phase index. Đây là hàm worker gọi nhiều nhất:
	// đọc theo cửa sổ nên RAM độc lập kích thước file, thay vì kéo cả text
	// của cả bài lên RAM bằng 1 câu.
	ListPagesRange(ctx context.Context, articleID int64, fromSeq, limit int) ([]*ArticlePage, error)
	// DeleteByArticleID dọn trang khi bài bị xoá giữa lúc đang trích. Sau khi
	// trích xong thì GIỮ lại làm nguồn cho phase index — chỉ purge mới xoá.
	DeleteByArticleID(ctx context.Context, articleID int64) error
	// DeleteByArticles như DeleteByArticleID nhưng nhiều bài 1 câu — purge
	// gom id trước khi xoá hẳn article row.
	DeleteByArticles(ctx context.Context, articleIDs []int64) error
	// DeleteOrphan xoá trang của bài KHÔNG còn tồn tại (đã xoá hẳn) — janitor
	// bắt trường hợp job extract chết giữa lúc ghi. Trả số row đã xoá.
	// Không đụng trang của bài còn sống kể cả bài đang extract dở.
	DeleteOrphan(ctx context.Context) (int64, error)
}
