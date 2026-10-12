package synthesis

import (
	"knowledge_ingestion/src/agent/prompts"
)

// Prompts gom prompt load từ file lúc startup — sửa file, restart app là ăn,
// không cần build lại. System là instruction của agent chat (Eino ChatModelAgent)
// và là hợp đồng trích dẫn [^n] với frontend. Không có bản mặc định trong code.
type Prompts struct {
	// System thay system prompt chat (instruction agent).
	System string
}

// chatSystemFile là tên file system prompt chat trong thư mục prompt.
const chatSystemFile = "chat_system.md"

// LoadPrompts đọc prompt từ dir lúc startup — ủy quyền cho package prompts
// dùng chung (cùng quy ước fail-fast với prompt classify).
func LoadPrompts(dir string) (Prompts, error) {
	system, err := prompts.Load(dir, chatSystemFile)
	if err != nil {
		return Prompts{}, err
	}
	return Prompts{System: system}, nil
}
