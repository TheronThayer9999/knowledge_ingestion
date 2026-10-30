package config

import "testing"

// TestNormalizeScanOCRKeepsPipelineOCREnTact là khoá AN TOÀN của quyết định tách
// hai đường OCR (§0.7): chuẩn hoá scan_ocr tuyệt đối không được sửa các giá trị
// ocr.* mà pipeline nạp kho đang dùng. Một lần chỉnh ngàm cho tab Quét mà âm
// thầm đổi PSM/DPI của đường trích nạp kho là lỗi rất khó truy.
func TestNormalizeScanOCRKeepsPipelineOCREnTact(t *testing.T) {
	cfg := &appConfig{OCR: OCRConfig{
		Binary: `C:\ocr\tesseract.exe`, RenderBinary: `D:\pop\pdftoppm.exe`,
		Lang: "vie+eng", PSM: 3, RenderDPI: 200, MinTextChars: 150, TimeoutSec: 45,
	}}
	before := cfg.OCR
	normalizeScanOCR(cfg)
	if cfg.OCR != before {
		t.Fatalf("ocr.* bị scan_ocr ghi đè:\n trước %+v\n sau  %+v", before, cfg.OCR)
	}
	// scan_ocr rỗng thì mượn đường dẫn binary của cùng một máy (chỉ cài
	// Tesseract/poppler một lần) nhưng không đi ngược lại.
	if cfg.ScanOCR.Binary != before.Binary || cfg.ScanOCR.RenderBinary != before.RenderBinary {
		t.Fatalf("scan_ocr không mượn đường dẫn: %+v", cfg.ScanOCR)
	}
	// Lang thì PHẢI tự quyết: mặc định "vie" chứ không thừa kế "vie+eng" của
	// pipeline, vì máy thiếu gói eng làm MỌI trang của job fail.
	if cfg.ScanOCR.Lang != "vie" {
		t.Fatalf("scan_ocr.lang = %q, muốn \"vie\"", cfg.ScanOCR.Lang)
	}
}

func TestNormalizeScanOCRDefaults(t *testing.T) {
	cfg := &appConfig{}
	normalizeScanOCR(cfg)
	s := cfg.ScanOCR
	if s.Engine != "tesseract" || s.Binary != "tesseract" || s.RenderBinary != "pdftoppm" {
		t.Fatalf("defaults đường dẫn/engine sai: %+v", s)
	}
	if s.PSM != 6 || s.RenderDPI != 300 || s.PreviewDPI != 150 ||
		s.PreviewJPEG != 80 || s.OutputJPEGQuality != 80 {
		t.Fatalf("defaults thông số render sai: %+v", s)
	}
	if s.ThresholdChar != 40 || s.MinConfWord != 70 || s.MaxAttempts != 3 || s.Parallel != 2 {
		t.Fatalf("defaults ngưỡng/ngân sách sai: %+v", s)
	}
	if s.MaxPages != 300 || s.MaxSizeMB != 200 || s.MaxUploadMB != 500 {
		t.Fatalf("defaults trần job sai: %+v", s)
	}
	if s.TempTTLHours != 24 || s.PushedTTLHours != 168 {
		t.Fatalf("defaults TTL sai: %+v", s)
	}
	// Tính năng TẮT theo mặc định: thiếu section thì API báo 400 chỉ đúng tên
	// công tắc, còn máy chủ không tự nhiên bật OCR chạy ngốn CPU.
	if s.Enabled {
		t.Fatal("scan_ocr.enabled phải mặc định tắt")
	}
	// Trần của một trang phải chứa trần một lần gọi engine.
	if s.PageTimeout < s.Timeout {
		t.Fatalf("page_timeout %d < timeout %d", s.PageTimeout, s.Timeout)
	}
}

func TestNormalizeScanOCRClampsAndKeepsUserValues(t *testing.T) {
	// Giá trị hợp lệ thì giữ nguyên những gì người vận hành gõ.
	cfg := &appConfig{ScanOCR: ScanOCRConfig{
		Enabled: true, Engine: "Paddle ", Lang: "vie+eng", PSM: 3,
		Timeout: 30, PageTimeout: 300, Parallel: 4, MinConfWord: 85, Binary: `X:\tess.exe`,
	}}
	normalizeScanOCR(cfg)
	s := cfg.ScanOCR
	if !s.Enabled || s.Lang != "vie+eng" || s.PSM != 3 || s.PageTimeout != 300 ||
		s.Parallel != 4 || s.MinConfWord != 85 || s.Binary != `X:\tess.exe` {
		t.Fatalf("ghi đè giá trị người dùng: %+v", s)
	}
	if s.Engine != "paddle" {
		t.Fatalf("engine = %q, muốn \"paddle\" (tường minh, không rơi về tesseract)", s.Engine)
	}
	// Timeout 30 nhỏ hơn PageTimeout thì PageTimeout giữ nguyên; ngược lại, trang
	// phải được OCR đủ lâu để không cắt giữa lúc Tesseract chạy.
	cfg2 := &appConfig{ScanOCR: ScanOCRConfig{Timeout: 600, PageTimeout: 10}}
	normalizeScanOCR(cfg2)
	if cfg2.ScanOCR.Timeout != 600 || cfg2.ScanOCR.PageTimeout != 600 {
		t.Fatalf("page_timeout phải >= timeout: %+v", cfg2.ScanOCR)
	}
	// Số vô lý bị kẹp về cận — để lọt thì thành DPI 0 (pdftoppm lỗi) hoặc
	// parallel 500 (sập máy).
	cfg3 := &appConfig{ScanOCR: ScanOCRConfig{PSM: 99, RenderDPI: 100000, MinConfWord: 900, Parallel: 400}}
	normalizeScanOCR(cfg3)
	s3 := cfg3.ScanOCR
	if s3.PSM != 13 || s3.RenderDPI != 600 || s3.MinConfWord != 100 || s3.Parallel != 8 {
		t.Fatalf("kẹp cận thất bại: %+v", s3)
	}
}

func TestClampInt(t *testing.T) {
	// <= 0 được hiểu là "không khai báo" (JSON thiếu field cho ra 0), nên luôn
	// về default trước khi kẹp — nếu không thì một giá trị 0 hợp lệ ở cột
	// min_conf_word sẽ bị kẹp lên 1 và khác hẳn ý người dùng.
	for _, tc := range []struct {
		in, def, lo, hi, want int
	}{
		{in: 0, def: 300, lo: 72, hi: 600, want: 300},
		{in: -5, def: 300, lo: 72, hi: 600, want: 300},
		{in: 10, def: 300, lo: 72, hi: 600, want: 72},
		{in: 99999, def: 300, lo: 72, hi: 600, want: 600},
		{in: 250, def: 300, lo: 72, hi: 600, want: 250},
	} {
		v := tc.in
		clampInt(&v, tc.def, tc.lo, tc.hi)
		if v != tc.want {
			t.Fatalf("clampInt(%d, def %d, [%d,%d]) = %d, muốn %d", tc.in, tc.def, tc.lo, tc.hi, v, tc.want)
		}
	}
}
