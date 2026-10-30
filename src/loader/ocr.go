package loader

import (
	"time"

	"knowledge_ingestion/src/common/extractor"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/config"

	"go.uber.org/fx"
)

// documentReaderOptions gom cấu hình đọc ảnh/tài liệu của API — VLM (qwen-vl,
// lazy) và OCR dùng chung với worker. Gom ở đây để loadAdapter không phình
// thêm một dòng mỗi lần thêm đầu đọc mới.
func documentReaderOptions() []fx.Option {
	return append(vlmOptions(), fx.Provide(newOCRConfig))
}

// newOCRConfig map config.GetOCR() sang contract của extractor — extractor
// không phụ thuộc config, nên cả worker lẫn API dùng chung một chỗ đổi.
//
// Worker dùng để OCR lúc nạp tài liệu; API dùng cho tab Quét (xem trước OCR
// đồng bộ). Cùng một struct cấu hình nên bật/tắt hay đổi binary chỉ sửa JSON.
func newOCRConfig(cfg config.IConfig) extractor.OCRConfig {
	o := cfg.GetOCR()
	if o.Enabled && o.RenderBinary == "" {
		// Warn lúc boot thay vì để PDF scan failed thầm lặng sau 10 lần thử —
		// người vận hành thấy ngay là thiếu renderer.
		logs.Warnw("ocr bật nhưng thiếu render_binary — trang PDF scan sẽ bị skip",
			"lang", o.Lang)
	}
	return extractor.OCRConfig{
		Enabled:      o.Enabled,
		Binary:       o.Binary,
		Lang:         o.Lang,
		Timeout:      time.Duration(o.TimeoutSec) * time.Second,
		RenderBinary: o.RenderBinary,
		RenderDPI:    o.RenderDPI,
		MinTextChars: o.MinTextChars,
		PSM:          o.PSM,
	}
}
