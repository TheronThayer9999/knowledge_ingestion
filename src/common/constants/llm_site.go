package constants

// Nhãn "ai gọi LLM" (phase) gắn vào ctx qua utils.WithLLMSite rồi đi theo
// request HTTP xuống transport của từng provider — nhờ vậy dòng log HTTP khớp
// được với dashboard provider: 1 lượt chat sinh NHIỀU request (classify + N
// vòng tool + retry + tóm tắt lịch sử), nên chỉ đếm ở tầng service là không
// đủ để giải thích vì sao provider vẫn thấy request sau khi UI báo xong.
const (
	// LLMSiteCompile worker phase compile tóm tắt từng mục tài liệu.
	LLMSiteCompile = "compile"
	// LLMSiteChat 1 lượt chat: classifier đã tách riêng, còn đây là các vòng
	// gọi model của agent (tool loop, retry, tóm tắt lịch sử).
	LLMSiteChat = "chat"
	// LLMSiteClassify vòng 1 phân loại câu hỏi — 1 request cho mỗi câu được
	// phân loại, không phải "call của compile".
	LLMSiteClassify = "classify"
)
