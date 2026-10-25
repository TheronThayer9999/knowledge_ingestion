package extractor

import "context"

// Adapter của từng định dạng — mỗi cái là một "tool" độc lập, chỉ gọi đúng hàm
// trích của file định dạng đó (docx.go, xlsx.go, pptx.go, pdf.go, ocr.go).
//
// MUỐN ĐỔI ENGINE HAY THÊM ĐỊNH DẠNG: sửa danh sách trong defaultReaders() —
// thay struct bằng bản dùng thư viện (excelize, unioffice...) hoặc ReaderFunc.
// Xem doc block đầu reader.go. Không sửa ExtractRawPages, không thêm bảng đuôi
// thứ hai. Các định dạng KHÔNG nằm trong danh sách này thì lỗi ErrUnsupported.

// defaultReaders là CHỖ THAY THẾ duy nhất: thứ tự trong slice quyết định reader
// nào thắng khi trùng đuôi (sau thắng trước).
func defaultReaders() []Reader {
	return []Reader{
		textReader{},
		docxReader{},
		xlsxReader{},
		pptxReader{},
		pdfReader{},
		imageReader{},
	}
}

// defaultRegistry là registry mà ExtractRawPages dùng. Bất biến sau khi dựng —
// muốn cắm thêm/đổi reader thì dùng DefaultRegistry().With(...) (bản sao) hoặc
// sửa defaultReaders().
var defaultRegistry = NewRegistry(defaultReaders()...)

// DefaultRegistry trả registry mặc định (chỉ đọc).
func DefaultRegistry() *Registry { return defaultRegistry }

// textReader: văn bản thuần, đọc TUẦN TỰ theo dòng nên RAM hằng số.
type textReader struct{}

func (textReader) Info() ReaderInfo {
	return ReaderInfo{
		Name:        "read_text",
		Description: "Trích text thuần (.txt/.md/.csv/.json) theo dòng, chia mảnh 256KB.",
		Exts:        []string{".txt", ".md", ".csv", ".json"},
	}
}

func (textReader) Read(_ context.Context, req ReadRequest, visit func(Page) error) error {
	return plainTextPieces(req.Data, visit)
}

type docxReader struct{}

func (docxReader) Info() ReaderInfo {
	return ReaderInfo{
		Name:        "read_docx",
		Description: "Trích Word (.docx): đoạn văn, bảng pipe, tiêu đề mục; bỏ track-change và mã field.",
		Exts:        []string{".docx"},
	}
}

func (docxReader) Read(_ context.Context, req ReadRequest, visit func(Page) error) error {
	text, err := extractDocx(req.Data)
	if err != nil {
		return err
	}
	return visitPieces(0, text, visit)
}

type xlsxReader struct{}

func (xlsxReader) Info() ReaderInfo {
	return ReaderInfo{
		Name:        "read_xlsx",
		Description: "Trích Excel (.xlsx): mỗi sheet một khối bảng pipe, giữ vị trí cột; chỉ lấy giá trị đã lưu.",
		Exts:        []string{".xlsx"},
	}
}

func (xlsxReader) Read(_ context.Context, req ReadRequest, visit func(Page) error) error {
	text, err := extractXlsx(req.Data)
	if err != nil {
		return err
	}
	return visitPieces(0, text, visit)
}

type pptxReader struct{}

func (pptxReader) Info() ReaderInfo {
	return ReaderInfo{
		Name:        "read_pptx",
		Description: "Trích PowerPoint (.pptx): mỗi slide một khối, sắp theo số slide; bỏ slide không có chữ.",
		Exts:        []string{".pptx"},
	}
}

func (pptxReader) Read(_ context.Context, req ReadRequest, visit func(Page) error) error {
	text, err := extractPptx(req.Data)
	if err != nil {
		return err
	}
	return visitPieces(0, text, visit)
}

type pdfReader struct{}

func (pdfReader) Info() ReaderInfo {
	return ReaderInfo{
		Name:        "read_pdf",
		Description: "Trích PDF theo TỪNG trang (poppler), OCR trang scan, resume theo trang đã có.",
		Exts:        []string{".pdf"},
	}
}

func (pdfReader) Read(ctx context.Context, req ReadRequest, visit func(Page) error) error {
	return extractPDFPages(ctx, req.Data, extractConfig{ocr: req.OCR, layout: req.Layout}, req.Have, visit)
}

type imageReader struct{}

func (imageReader) Info() ReaderInfo {
	return ReaderInfo{
		Name:        "read_image",
		Description: "OCR ảnh (.jpg/.jpeg/.png/.webp) bằng Tesseract; ảnh trắng thì lỗi, không đánh done giả.",
		Exts:        []string{".jpg", ".jpeg", ".png", ".webp"},
	}
}

func (imageReader) Read(ctx context.Context, req ReadRequest, visit func(Page) error) error {
	text, err := OCRImage(ctx, req.OCR, req.Key, req.Data)
	if err != nil {
		return err
	}
	return visit(Page{Num: 0, Seq: 1, Text: text})
}
