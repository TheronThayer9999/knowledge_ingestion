package loader

import (
	"time"

	"knowledge_ingestion/src/agent/prompts"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"
	"knowledge_ingestion/src/infrastructure/vlm/ollama"

	"go.uber.org/fx"
)

// vlmOptions dựng adapter đọc ảnh và bind 2 VAI HẸP (ISP):
//   - domain.IVLMReader → pipeline extract (đọc cả trang phức tạp)
//   - domain.IVLMAsker  → tool read_page_visual của agent (hỏi 1 con số)
//
// Cùng một instance, hai kiểu; consumer không thấy method của vai còn lại.
//
// KHÔNG có fx.Invoke: chưa consumer nào dùng nên provider giữ lazy — app boot
// y như cũ kể cả khi Ollama không chạy. Đây là chủ ý: thêm một fx.Invoke cho
// VLM sẽ biến Ollama thành phụ thuộc bắt buộc để boot, trong khi tính năng còn
// tắt (vlm.enabled = false).
func vlmOptions() []fx.Option {
	return []fx.Option{
		fx.Provide(func(cfg config.IConfig) ollama.Config {
			v := cfg.GetVLM()
			if !v.Enabled {
				logs.Debug("vlm đang tắt — không consumer nào được dùng reader cho tới khi bật vlm.enabled")
			}
			return ollama.Config{
				BaseURL:       v.BaseURL,
				Model:         v.Model,
				APIKey:        v.APIKey,
				Timeout:       time.Duration(v.TimeoutSec) * time.Second,
				MaxTokens:     v.MaxTokens,
				Temperature:   v.Temperature,
				MaxImageBytes: v.MaxImageBytes,
			}
		}),
		// Prompt load lúc startup từ prompts/*.md — thiếu file thì fail-fast
		// đúng như chat_system/classify_system, không có bản mặc định trong
		// code để prompt cũ và code lệch nhau âm thầm.
		fx.Provide(func(cfg config.IConfig) (ollama.Prompts, error) {
			dir := cfg.GetAgent().PromptDir
			transcribe, err := prompts.Load(dir, "vlm_transcribe.md")
			if err != nil {
				return ollama.Prompts{}, err
			}
			ask, err := prompts.Load(dir, "vlm_ask.md")
			if err != nil {
				return ollama.Prompts{}, err
			}
			return ollama.Prompts{Transcribe: transcribe, Ask: ask}, nil
		}),
		fx.Provide(ollama.NewReader),
		fx.Provide(func(r *ollama.Reader) domain.IVLMReader { return r }),
		fx.Provide(func(r *ollama.Reader) domain.IVLMAsker { return r }),
	}
}
