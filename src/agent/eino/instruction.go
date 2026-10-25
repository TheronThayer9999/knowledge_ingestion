package eino

import (
	"context"
	"strings"
	"time"

	"knowledge_ingestion/src/controller/dtos"
)

// Prompt chat nằm ở 2 file: `chat_system.md` là luật chung cho mọi câu hỏi,
// `chat_system_sheets.md` là luật riêng cho bảng tính (Excel). InstructionSet
// giữ cả hai và quyết định LÚC CHẠY có nối fragment bảng tính vào hay không —
// câu hỏi về hợp đồng không phải đọc 15 luật sheet vô ích, còn câu hỏi về
// Excel thì luật không bị loãng giữa 250 dòng luật chung.
type InstructionSet struct {
	// Base là prompt chung, bắt buộc.
	Base string
	// Sheets là fragment luật bảng tính, chỉ nối vào khi câu hỏi liên quan.
	Sheets string
}

// todayPlaceholder là placeholder duy nhất còn lại trong prompt. Trước đây
// không chỗ nào render nó nên model nhận nguyên văn chuỗi "{ngày_hiện_tại}" —
// quy tắc "đối chiếu ngày hôm nay" ở mục 11 vì thế mất căn cứ.
const todayPlaceholder = "{ngày_hiện_tại}"

// todayLayout là dd/mm/yyyy — khớp cách người dùng Việt viết ngày trong câu hỏi.
const todayLayout = "02/01/2006"

// PromptVars là phần instruction thay đổi theo từng lượt chat (khác phần tĩnh
// đọc từ file lúc startup).
type PromptVars struct {
	// Today render vào {ngày_hiện_tại}; rỗng thì lấy ngày hôm nay.
	Today string
	// Sheets bật fragment "Quy tắc riêng: bảng tính".
	Sheets bool
}

// Render trả instruction cuối cùng của lượt: thay placeholder ngày, nối
// fragment bảng tính khi cần. Base rỗng thì trả rỗng — agent không có
// instruction vẫn chạy (Eino cho phép), nhưng loader đã fail-fast từ trước.
func (s InstructionSet) Render(v PromptVars) string {
	today := strings.TrimSpace(v.Today)
	if today == "" {
		today = time.Now().Format(todayLayout)
	}
	out := strings.ReplaceAll(s.Base, todayPlaceholder, today)
	sheets := strings.TrimSpace(s.Sheets)
	if v.Sheets && sheets != "" {
		out = strings.TrimRight(out, "\n") + "\n\n" + sheets
	}
	return out
}

// DefaultPromptVars là biến cho lượt không đi qua Run (test, chẩn đoán) — vẫn
// render được ngày, chỉ không có luật bảng tính.
func DefaultPromptVars() PromptVars { return PromptVars{Today: time.Now().Format(todayLayout)} }

// promptVarsKey là key ctx private của package — không ai ngoài package ghi
// đè được biến prompt của lượt.
type promptVarsKey struct{}

// WithPromptVars gắn biến prompt của lượt vào ctx. Instruction của
// ChatModelAgent cố định lúc dựng agent, nên đây là cách đưa dữ liệu theo lượt
// tới middleware BeforeAgent (xem promptInjector trong agent.go).
func WithPromptVars(ctx context.Context, v PromptVars) context.Context {
	return context.WithValue(ctx, promptVarsKey{}, v)
}

// promptVarsFrom đọc biến prompt của lượt; thiếu thì dùng mặc định để
// instruction luôn có ngày cụ thể, không lộ placeholder ra model.
func promptVarsFrom(ctx context.Context) PromptVars {
	if v, ok := ctx.Value(promptVarsKey{}).(PromptVars); ok {
		return v
	}
	return DefaultPromptVars()
}

// spreadsheetMarkers là dấu hiệu câu hỏi liên quan bảng tính. Cố ý bắt cả
// tiếng Anh lẫn tiếng Việt vì người dùng gõ lẫn lộn; thà bắt thừa (model đọc
// thêm vài dòng luật vô hại) còn hơn bắt thiếu (model không biết phải đọc
// sheet theo hàng, trả lời từ mảnh bảng vỡ cột).
//
// "bảng " (kèm dấu cách) bắt các câu như "bảng lương có bao nhiêu người" —
// người dùng không nói "Excel" nhưng dữ liệu họ hỏi nằm trong sheet.
var spreadsheetMarkers = []string{
	"excel", "xlsx", "xls", "spreadsheet", "sheet",
	"trang tính", "bảng tính", "bảng dữ liệu", "bảng ",
}

// spreadsheetHistoryMessages là số message gần nhất còn được soi (cả user lẫn
// assistant) — câu nối tiếp kiểu "sheet đó có bao nhiêu hàng" thường chỉ dựa
// vào 1-2 lượt trước, và câu trả lời của trợ lý cũng nhắc tên sheet nên đó là
// tín hiệu tốt không kém.
const spreadsheetHistoryMessages = 4

// IsSpreadsheetQuestion đoán câu hỏi có liên quan bảng tính hay không để nạp
// fragment luật Excel. Heuristic từ khóa, cố ý KHÔNG gọi thêm LLM: phân loại
// bằng model sẽ tốn 1 lượt cho mọi câu hỏi, trong khi tín hiệu ở đây rất rõ.
// Câu nối tiếp không có từ khóa vẫn bắt được nhờ soi vài message gần nhất.
func IsSpreadsheetQuestion(message string, history []dtos.ChatHistoryItem) bool {
	if containsSpreadsheetMarker(message) {
		return true
	}
	start := len(history) - spreadsheetHistoryMessages
	if start < 0 {
		start = 0
	}
	for i := len(history) - 1; i >= start; i-- {
		if containsSpreadsheetMarker(history[i].Content) {
			return true
		}
	}
	return false
}

func containsSpreadsheetMarker(s string) bool {
	lower := strings.ToLower(s)
	for _, m := range spreadsheetMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}
