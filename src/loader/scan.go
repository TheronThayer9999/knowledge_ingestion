package loader

import (
	"time"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/scanocr"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/services"
	"knowledge_ingestion/src/infrastructure/repository"

	"go.uber.org/fx"
)

// scanFeatureOptions gom mọi provider phía API của tab Quét: bộ đọc ảnh/tài
// liệu (VLM + cấu hình OCR xem trước, khai ở ocr.go), hàng đợi quét theo trang
// và service nghiệp vụ của nó.
//
// Gom thành một nhóm vì loadAdapter đã sát ngân sách dòng (guardrail
// src/quality/function_budget_test.go), và vì V1 còn thêm provider vào đúng
// feature này (region re-OCR, sửa tay) — chỗ sửa chỉ là hàm này.
func scanFeatureOptions() []fx.Option {
	return append(documentReaderOptions(),
		fx.Provide(newScanOCRConfig, newScanJobOptions),
		fx.Provide(repository.NewOCRJobRepository))
}

// newScanOCRConfig map config.GetScanOCR() sang hợp đồng của package scanocr.
//
// KHÁC newOCRConfig (đường nạp kho) ở chỗ nó mang thêm DPI render, ngưỡng
// "trang đã có chữ" và trần retry của TÍNH NĂNG — hai mục tiêu khác nhau nên
// hai struct, dù cùng mượn đường dẫn binary trên một máy (xem §0.7 của plan).
// API dùng nó để đo text layer lúc tạo job, worker dùng để render + OCR.
func newScanOCRConfig(cfg config.IConfig) scanocr.Config {
	s := cfg.GetScanOCR()
	return scanocr.Config{
		Enabled:           s.Enabled,
		Engine:            s.Engine,
		Binary:            s.Binary,
		Lang:              s.Lang,
		PSM:               s.PSM,
		Timeout:           time.Duration(s.Timeout) * time.Second,
		PageTimeout:       time.Duration(s.PageTimeout) * time.Second,
		RenderBinary:      s.RenderBinary,
		RenderDPI:         s.RenderDPI,
		PreviewDPI:        s.PreviewDPI,
		TextLayerMinChars: s.ThresholdChar,
		MinConfWord:       s.MinConfWord,
		MaxAttempts:       s.MaxAttempts,
		Parallel:          s.Parallel,
	}.Normalize()
}

// newScanJobOptions map phần TRẦN của tính năng (không phải của engine) sang
// struct service — cùng khuôn PurgeOptions: service không import package config.
// TTL lấy từ config, số 0 thì giữ mặc định trong constants để một dòng JSON
// thiếu không vô tình biến bản quét thành vĩnh viễn.
func newScanJobOptions(cfg config.IConfig) services.ScanJobOptions {
	s := cfg.GetScanOCR()
	ttl := func(hours int, def time.Duration) time.Duration {
		if hours <= 0 {
			return def
		}
		return time.Duration(hours) * time.Hour
	}
	return services.ScanJobOptions{
		Enabled: s.Enabled,
		Lang:    s.Lang,
		DPI:     s.RenderDPI,
		PSM:     s.PSM,
		Engine:  s.Engine,
		// MaxSizeMB là trần CỦA TÀI LIỆU được quét; MaxUploadMB (lớn hơn) chỉ là
		// trần body HTTP — phần dư đó cho header multipart, không phải để cho
		// file to hơn lọt vào hàng đợi.
		MaxUploadBytes: int64(s.MaxSizeMB) << 20,
		MaxPages:       s.MaxPages,
		TempTTL:        ttl(s.TempTTLHours, constants.SCAN_TMP_TTL),
		PushedTTL:      ttl(s.PushedTTLHours, constants.SCAN_PUSHED_TTL),
		PresignTTL:     constants.PRESIGN_DOWNLOAD_EXPIRY,
	}
}
