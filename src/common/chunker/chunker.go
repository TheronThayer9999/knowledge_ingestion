package chunker

import (
	"strings"
	"unicode/utf8"
)

// Option tinh chỉnh Split — zero value là dùng default.
type Option struct {
	// ChunkSize là số rune tối đa mỗi chunk. <=0 thì dùng DefaultChunkSize.
	ChunkSize int
	// Overlap là số rune gối giữa 2 chunk liên tiếp để không đứt ngữ cảnh.
	// Kẹp về [0, ChunkSize).
	Overlap int
	// Separators là thứ tự dấu cắt ưu tiên, từ "sạch" (đoạn văn) tới "vụn"
	// (từng ký tự). Nil thì dùng DefaultSeparators.
	Separators []string
}

const (
	// DefaultChunkSize vừa một đoạn văn bản ngắn — đủ ngữ cảnh cho embedding
	// mà không vượt context các model embed phổ biến.
	DefaultChunkSize = 1000
	// DefaultOverlap giữ ý câu bị cắt đôi ở biên chunk.
	DefaultOverlap = 200
)

// DefaultSeparators cắt từ biên lớn tới biên nhỏ: đoạn văn, dòng, từ, ký tự.
var DefaultSeparators = []string{"\n\n", "\n", " ", ""}

// Split cắt text thành chunk để embed — thuật toán recursive: thử dấu cắt
// "sạch" nhất trước, đoạn nào vẫn quá dài thì đệ quy cắt tiếp bằng dấu
// nhỏ hơn. Đo theo rune để không vỡ ký tự UTF-8 (tiếng Việt).
func Split(text string, opt Option) []string {
	size := opt.ChunkSize
	if size <= 0 {
		size = DefaultChunkSize
	}
	overlap := opt.Overlap
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= size {
		overlap = size - 1
	}
	seps := opt.Separators
	if len(seps) == 0 {
		seps = DefaultSeparators
	}
	return splitRecursive(strings.TrimSpace(text), size, overlap, seps)
}

func splitRecursive(text string, size, overlap int, seps []string) []string {
	if text == "" {
		return nil
	}
	if utf8.RuneCountInString(text) <= size {
		return []string{text}
	}
	sep, rest := seps[0], seps[1:]
	var parts []string
	if sep == "" {
		parts = splitRunes(text, size-overlap)
	} else {
		parts = splitBySep(text, sep, size-overlap)
	}
	var chunks []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if utf8.RuneCountInString(p) <= size || len(rest) == 0 {
			chunks = append(chunks, p)
			continue
		}
		chunks = append(chunks, splitRecursive(p, size, overlap, rest)...)
	}
	return mergeOverlap(chunks, size, overlap)
}

// splitBySep gom các đoạn con (cắt bởi sep) thành mảng, mỗi phần không quá
// limit rune — đoạn con đơn lẻ dài hơn limit thì kệ, tầng đệ quy xử tiếp.
func splitBySep(text, sep string, limit int) []string {
	var parts []string
	var cur []string
	curLen := 0
	flush := func() {
		if len(cur) > 0 {
			parts = append(parts, strings.Join(cur, sep))
			cur, curLen = nil, 0
		}
	}
	for _, s := range strings.Split(text, sep) {
		n := utf8.RuneCountInString(s)
		if curLen > 0 && curLen+len(sep)+n > limit {
			flush()
		}
		cur = append(cur, s)
		curLen += n + len(sep)
	}
	flush()
	return parts
}

// splitRunes cắt chuỗi thành lát limit rune — đường cùng khi không còn dấu cắt.
func splitRunes(text string, limit int) []string {
	if limit <= 0 {
		limit = 1
	}
	runes := []rune(text)
	var parts []string
	for i := 0; i < len(runes); i += limit {
		end := i + limit
		if end > len(runes) {
			end = len(runes)
		}
		parts = append(parts, string(runes[i:end]))
	}
	return parts
}

// mergeOverlap gộp các chunk kề nhau còn ngắn thành chunk đủ size, đồng thời
// chép overlap rune cuối chunk trước làm đầu chunk sau.
func mergeOverlap(chunks []string, size, overlap int) []string {
	if len(chunks) == 0 {
		return nil
	}
	var out []string
	var cur []string
	curLen := 0
	push := func() {
		if len(cur) > 0 {
			out = append(out, strings.Join(cur, " "))
			cur, curLen = nil, 0
		}
	}
	for _, ch := range chunks {
		n := utf8.RuneCountInString(ch)
		if curLen > 0 && curLen+1+n > size {
			push()
			if overlap > 0 && len(out) > 0 {
				tail := lastRunes(out[len(out)-1], overlap)
				cur = []string{tail}
				curLen = utf8.RuneCountInString(tail)
				if curLen+1+n > size {
					push()
				}
			}
		}
		cur = append(cur, ch)
		curLen += n + 1
	}
	push()
	return out
}

// lastRunes lấy tối đa n rune cuối chuỗi.
func lastRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}
