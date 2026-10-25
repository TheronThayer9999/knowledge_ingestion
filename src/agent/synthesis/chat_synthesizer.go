package synthesis

import (
	"knowledge_ingestion/src/agent/prompts"
)

// Prompts gom prompt load từ file lúc startup — sửa file, restart app là ăn,
// không cần build lại. System là instruction của agent chat (Eino ChatModelAgent)
// và là hợp đồng trích dẫn [^n] với frontend. Không có bản mặc định trong code.
type Prompts struct {
	// System thay system prompt chat (instruction agent) — luật chung cho
	// mọi câu hỏi.
	System string
	// Sheets là fragment luật bảng tính (Excel). KHÔNG nằm trong System:
	// agent chỉ nối fragment này vào instruction của lượt nào có câu hỏi liên
	// quan bảng tính (xem eino.InstructionSet / IsSpreadsheetQuestion), nên
	// câu hỏi về hợp đồng không phải đọc 15 dòng luật sheet vô ích.
	Sheets string
}

const (
	// chatSystemFile là tên file system prompt chat trong thư mục prompt.
	chatSystemFile = "chat_system.md"
	// chatSheetsFile là fragment luật bảng tính — thiếu file thì fail-fast
	// như prompt chính: agent sẽ âm thầm trả lời câu hỏi Excel bằng suy đoán
	// thay vì đọc sheet, đúng loại lỗi im lặng cần chặn từ lúc boot.
	chatSheetsFile = "chat_system_sheets.md"
)

// LoadPrompts đọc prompt từ dir lúc startup — ủy quyền cho package prompts
// dùng chung (cùng quy ước fail-fast với prompt classify).
func LoadPrompts(dir string) (Prompts, error) {
	system, err := prompts.Load(dir, chatSystemFile)
	if err != nil {
		return Prompts{}, err
	}
	sheets, err := prompts.Load(dir, chatSheetsFile)
	if err != nil {
		return Prompts{}, err
	}
	return Prompts{System: system, Sheets: sheets}, nil
}
