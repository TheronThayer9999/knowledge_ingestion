package chunker

import (
	"context"
	"strings"
)

// TemplateType là loại tài liệu để chọn chiến lược chunking.
type TemplateType string

const (
	TemplateGeneral TemplateType = "general"
	TemplateLaws    TemplateType = "laws"
	TemplateTable   TemplateType = "table"
	TemplatePaper   TemplateType = "paper"
	TemplateBook    TemplateType = "book"
	TemplateQA      TemplateType = "qa"
	TemplateManual  TemplateType = "manual"
)

// DetectType đoán loại tài liệu từ content_type + tên file. Rỗng/unknown →
// general (fallback recursive).
func DetectType(contentType, name string) TemplateType {
	ct := strings.ToLower(contentType)
	nm := strings.ToLower(name)
	switch {
	case strings.Contains(ct, "pdf") && (strings.Contains(nm, "luat") || strings.Contains(nm, "luật") ||
		strings.Contains(nm, "nghi dinh") || strings.Contains(nm, "nghị định") ||
		strings.Contains(nm, "thong tu") || strings.Contains(nm, "thông tư")):
		return TemplateLaws
	case strings.Contains(ct, "csv") || strings.Contains(ct, "spreadsheet") ||
		strings.Contains(ct, "excel") || strings.Contains(nm, ".xlsx") || strings.Contains(nm, ".csv"):
		return TemplateTable
	case strings.Contains(nm, "paper") || strings.Contains(nm, "bao cao") || strings.Contains(nm, "báo cáo"):
		return TemplatePaper
	}
	return TemplateGeneral
}

// NormalizeType chuẩn hóa chuỗi từ client/DB về TemplateType hợp lệ.
func NormalizeType(s string) TemplateType {
	switch TemplateType(strings.ToLower(strings.TrimSpace(s))) {
	case TemplateLaws, TemplateTable, TemplatePaper, TemplateBook, TemplateQA, TemplateManual:
		return TemplateType(strings.ToLower(strings.TrimSpace(s)))
	}
	return TemplateGeneral
}

// templateChunker bọc base chunker và áp chiến lược theo loại tài liệu.
// Chỉ Laws có xử lý riêng (cắt theo Điều/Khoản qua SplitBlocks + prepend
// header). Các loại khác delegate cho base — mở rộng sau.
type templateChunker struct {
	kind TemplateType
	base Chunker
}

// NewTemplate dựng chunker theo loại tài liệu, base là splitter nền
// (recursive/semantic/manual) dùng để cắt nhỏ trong từng block.
func NewTemplate(kind TemplateType, base Chunker) Chunker {
	if base == nil {
		return nil
	}
	return &templateChunker{kind: kind, base: base}
}

func (t *templateChunker) Split(ctx context.Context, text string) ([]string, error) {
	if t.kind != TemplateLaws {
		return t.base.Split(ctx, text)
	}
	blocks := SplitBlocks(text)
	var out []string
	for _, b := range blocks {
		header := HeaderFor(b.Heading)
		parts, err := t.base.Split(ctx, b.Text)
		if err != nil {
			return nil, err
		}
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if header != "" {
				out = append(out, header+p)
			} else {
				out = append(out, p)
			}
		}
	}
	if len(out) == 0 {
		return t.base.Split(ctx, text)
	}
	return out, nil
}

