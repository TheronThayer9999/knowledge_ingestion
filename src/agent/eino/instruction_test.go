package eino

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"knowledge_ingestion/src/controller/dtos"
)

// TestIsSpreadsheetQuestion khoá bộ dò "câu hỏi có liên quan bảng tính" — cái
// công tắc quyết định fragment luật Excel có được nạp hay không. Thiên về bắt
// thừa: câu "bảng lương" không nói Excel vẫn phải bật, vì dữ liệu nằm trong
// sheet; câu về Điều/khoản thì phải tắt để prompt không phình.
func TestIsSpreadsheetQuestion(t *testing.T) {
	cases := []struct {
		name    string
		message string
		history []dtos.ChatHistoryItem
		want    bool
	}{
		{name: "hỏi sheet", message: "File Excel này có những sheet nào?", want: true},
		{name: "hỏi tên file xlsx", message: "quy_che_luong.xlsx có cột phụ cấp không", want: true},
		{name: "tiếng anh", message: "How many rows in the payroll sheet?", want: true},
		{name: "bảng lương không nói excel", message: "Bảng lương tháng 9 có bao nhiêu nhân viên?", want: true},
		{name: "bảng tính", message: "Thông tin này nằm ở bảng tính nào?", want: true},
		{name: "câu chung về tài liệu", message: "Nghỉ phép năm được bao nhiêu ngày?", want: false},
		{name: "câu về điều khoản", message: "Điều 5 quy định gì về kỷ luật lao động?", want: false},
		{name: "chào hỏi", message: "Xin chào", want: false},
		{
			name:    "nối tiếp dựa vào lượt trước",
			message: "Còn sheet đó có bao nhiêu hàng?",
			history: []dtos.ChatHistoryItem{
				{Role: "user", Content: "File lương.xlsx có gì?"},
				{Role: "assistant", Content: "File có 3 sheet."},
			},
			want: true,
		},
		{
			name:    "history chỉ có assistant cũng đủ",
			message: "Còn số liệu đó thì sao?",
			history: []dtos.ChatHistoryItem{
				{Role: "assistant", Content: "Theo sheet Bảng lương, ..."},
			},
			want: true,
		},
		{
			name:    "history tài liệu thường",
			message: "Còn điều đó thì sao?",
			history: []dtos.ChatHistoryItem{
				{Role: "user", Content: "Hợp đồng thử việc quy định gì?"},
				{Role: "assistant", Content: "Theo Điều 3 ..."},
			},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSpreadsheetQuestion(tc.message, tc.history); got != tc.want {
				t.Fatalf("IsSpreadsheetQuestion(%q) = %v, muốn %v", tc.message, got, tc.want)
			}
		})
	}
}

// TestRenderInstructionFillsToday: placeholder ngày phải được thay mỗi lượt —
// trước đây không ai render nên model nhận nguyên văn "{ngày_hiện_tại}" và quy
// tắc đối chiếu hiệu lực ở mục 11 mất căn cứ.
func TestRenderInstructionFillsToday(t *testing.T) {
	set := InstructionSet{Base: "Hôm nay là {ngày_hiện_tại}.", Sheets: "# Quy tắc riêng"}
	got := set.Render(PromptVars{Today: "25/10/2026"})
	if strings.Contains(got, "{") {
		t.Fatalf("còn placeholder chưa render: %q", got)
	}
	if !strings.Contains(got, "25/10/2026") {
		t.Fatalf("thiếu ngày hiện tại: %q", got)
	}
}

// TestRenderInstructionAppendsSheetsOnlyWhenRelevant: đúng yêu cầu "chỉ đẩy
// prompt bảng tính khi câu hỏi liên quan Excel" — câu khác không được thấy
// fragment.
func TestRenderInstructionAppendsSheetsOnlyWhenRelevant(t *testing.T) {
	set := InstructionSet{Base: "luật chung", Sheets: "# Quy tắc riêng: bảng tính\n- đọc theo hàng"}

	off := set.Render(PromptVars{Today: "25/10/2026"})
	if strings.Contains(off, "đọc theo hàng") {
		t.Fatalf("câu không liên quan bảng tính mà vẫn nạp fragment: %q", off)
	}

	on := set.Render(PromptVars{Today: "25/10/2026", Sheets: true})
	if !strings.HasSuffix(on, "- đọc theo hàng") {
		t.Fatalf("câu liên quan bảng tính phải nối fragment vào cuối: %q", on)
	}
	if !strings.HasPrefix(on, "luật chung") {
		t.Fatalf("fragment không được thay thế luật chung: %q", on)
	}
}

// TestSheetRulesLiveOnlyInFragment khoá việc tách file: luật bảng tính chỉ nằm
// ở prompts/chat_system_sheets.md. Nếu ai đó dán lại luật sheet vào prompt
// chung thì câu hỏi về hợp đồng lại phải "đọc" chúng và cơ chế nạp có điều
// kiện mất tác dụng.
func TestSheetRulesLiveOnlyInFragment(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "prompts")
	baseRaw, err := os.ReadFile(filepath.Join(dir, "chat_system.md"))
	if err != nil {
		t.Fatalf("đọc chat_system.md: %v", err)
	}
	base := strings.ToLower(string(baseRaw))
	for _, w := range []string{"excel", "sheet", "bảng tính", "trang tính"} {
		if strings.Contains(base, w) {
			t.Errorf("chat_system.md còn luật bảng tính (%q) — chuyển sang chat_system_sheets.md", w)
		}
	}
	// Placeholder {label} cũ đã bỏ: classifier chạy sau khi agent xong nên
	// không có nhãn tại thời điểm dựng instruction.
	if strings.Contains(string(baseRaw), "{label}") {
		t.Error("chat_system.md còn placeholder chết {label}")
	}

	fragRaw, err := os.ReadFile(filepath.Join(dir, "chat_system_sheets.md"))
	if err != nil {
		t.Fatalf("đọc chat_system_sheets.md: %v", err)
	}
	frag := strings.ToLower(string(fragRaw))
	if !strings.Contains(frag, "sheet") {
		t.Error("fragment bảng tính không nhắc tới sheet — không dạy được gì")
	}
	if !strings.Contains(frag, "list_sheets") && !strings.Contains(frag, "liệt kê") {
		t.Error("fragment phải nhắc bước liệt kê sheet trước khi đọc")
	}
}
