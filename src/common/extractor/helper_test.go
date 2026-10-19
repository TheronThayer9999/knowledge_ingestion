package extractor

import (
	"context"
	"io"
)

// collectPages bọc callback của ExtractPages thành hàm trả về []Page — chỉ
// dùng trong test. Test KHÔNG được giữ tham chiếu tới struct trong slice
// gốc của extractor: với PDF, extractPDFPages xoá text ngay sau khi visit
// để giải phóng bộ nhớ, nên chỉ bản SAO truyền vào visit là an toàn.
func collectPages(key string, r io.Reader, ocr OCRConfig) ([]Page, error) {
	var pages []Page
	err := ExtractPages(context.Background(), key, r, ocr, func(p Page) error {
		pages = append(pages, p)
		return nil
	})
	return pages, err
}
