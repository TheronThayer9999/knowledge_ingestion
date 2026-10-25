package services

import (
	"sort"
	"testing"

	"knowledge_ingestion/src/common/extractor"
)

// TestUploadWhitelistSupportedByExtractor chặn đúng lỗi cũ: whitelist upload
// nhận 13 đuôi trong khi extractor chỉ có 10 nhánh (.xlsx/.pptx/.zip lọt
// lưới). Hậu quả là upload thành công, worker trả ErrUnsupported, bài nằm ở
// failed mà người dùng chỉ thấy badge "Thất bại". Thêm đuôi vào allowedExts
// mà quên dạy extractor đọc ⇒ test này đỏ ngay.
func TestUploadWhitelistSupportedByExtractor(t *testing.T) {
	exts := make([]string, 0, len(allowedExts))
	for ext := range allowedExts {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	for _, ext := range exts {
		if !extractor.Supports("tep" + ext) {
			t.Errorf("allowedExts nhận %q nhưng extractor không trích được — upload sẽ thành công rồi failed im lặng", ext)
		}
	}
}

// TestZipRejectedByUpload: .zip là archive, không phải tài liệu. Trích nó là
// mở đường zip-bomb/duyệt file lạ mà không có giá trị nghiệp vụ nào.
func TestZipRejectedByUpload(t *testing.T) {
	if _, ok := allowedExts[".zip"]; ok {
		t.Fatal(".zip không được nằm trong whitelist upload")
	}
}
