package seaweedfs

import (
	"strconv"
	"strings"
	"unicode"
)

// Bộ mã này lo phần khó nhất của chữ ký tải file: nhét TÊN FILE tiếng Việt vào
// header Content-Disposition sao cho browser nào cũng hiểu.
//
// Hai cái bẫy đã gặp: (1) link presign là cross-origin nên thuộc tính
// `download` của thẻ <a> bị browser bỏ qua — không có "attachment" là file mở
// ra ở tab mới thay vì tải; (2) header HTTP chỉ là ASCII, dán trực tiếp
// "báo cáo.pdf" vào là hỏng byte, nên tên thật phải mã theo RFC 5987
// (`filename*=UTF-8''…`) và đồng thời gửi bản ASCII `filename="…"`.
//
// Browser cũ lẫn mới đều đọc cả hai field; cái nào hỗ trợ thì ưu tiên
// `filename*` (đúng dấu tiếng Việt), bản ASCII chỉ là phao cho client cổ —
// tên thuần dấu sẽ rơi về "download", thà vậy còn hơn gửi header sai cú pháp.

// attrChar là tập ký tự RFC 5987 KHÔNG cần escape khi mã ext-value.
func attrChar(r rune) bool {
	switch r {
	case '!', '#', '$', '&', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// encodeExtValue percent-encode chuỗi UTF-8 theo luật ext-value của RFC 5987:
// giữ nguyên attr-char, mọi thứ khác (kể cả dấu cách, dấu câu, ký tự có dấu)
// thành %XX theo từng byte của bản mã UTF-8.
func encodeExtValue(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x80 && attrChar(rune(c)) {
			b.WriteByte(c)
			continue
		}
		const hex = "0123456789ABCDEF"
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&0x0F])
	}
	return b.String()
}

// asciiFilename cắt tên về đúng ký tự ASCII an toàn cho quoted-string; chuỗi
// rỗng (thuần dấu) thì fallback sang tên mặc định để header vẫn hợp lệ.
func asciiFilename(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c <= 0x7E && c != '"' && c != '\\' {
			b.WriteByte(c)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "download"
	}
	return out
}

// buildContentDisposition dựng giá trị header: attachment + bản ASCII (tương
// thích browser cổ) + bản UTF-8 mã RFC 5987 (giữ nguyên dấu tiếng Việt).
func buildContentDisposition(filename string) string {
	return "attachment; filename=" + strconv.Quote(asciiFilename(filename)) +
		"; filename*=UTF-8''" + encodeExtValue(filename)
}

// buildInlineDisposition tương tự nhưng dùng "inline" để browser preview PDF
// trong iframe thay vì tải xuống.
func buildInlineDisposition(filename string) string {
	return "inline; filename=" + strconv.Quote(asciiFilename(filename)) +
		"; filename*=UTF-8''" + encodeExtValue(filename)
}
