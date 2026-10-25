package extractor

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// extractPptx trích text từ PowerPoint (.pptx) chỉ bằng stdlib — cùng họ
// zip+XML với docx/xlsx. Slide là văn bản in bình thường nên đọc trực tiếp;
// ảnh/biểu đồ nhúng không trích (ca đó đi qua tool agent — RAG §6.12).
//
// Mỗi slide 1 khối mở đầu "## Slide <n>" để chunk mang ngữ cảnh. Số là số THẬT
// của slide và sắp theo SỐ (slide10 sau slide2). Slide chỉ có ảnh thì bỏ, không
// sinh khối rỗng.
func extractPptx(r io.Reader) (string, error) {
	zr, err := openOfficeZip(r, "pptx")
	if err != nil {
		return "", err
	}
	slides := numberedEntries(zr, "ppt/slides/slide")
	if len(slides) == 0 {
		return "", fmt.Errorf("%w: file pptx không có slide nào", ErrInvalid)
	}
	var blocks []string
	for _, s := range slides {
		data, err := readZipEntry(s.file)
		if err != nil {
			return "", err
		}
		paras, err := pptxParagraphs(data)
		if err != nil {
			return "", fmt.Errorf("%w: parse %s: %v", ErrInvalid, s.file.Name, err)
		}
		if len(paras) == 0 {
			continue
		}
		blocks = append(blocks, fmt.Sprintf("## Slide %d\n%s", s.num, strings.Join(paras, "\n\n")))
	}
	return strings.TrimSpace(strings.Join(blocks, "\n\n")), nil
}

// pptxParagraphs tách đoạn văn của 1 slide: mỗi <a:p> một khối, <a:br> thành
// xuống dòng, còn lại chỉ lấy CharData nằm trong đoạn (mã định dạng là attribute
// nên không lẫn vào). Đoạn rỗng bị bỏ — slide mở đầu bằng textbox rỗng là
// chuyện thường, giữ lại chỉ sinh khối trắng.
func pptxParagraphs(data []byte) ([]string, error) {
	var paras []string
	var cur strings.Builder
	inP := false
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				inP = true
				cur.Reset()
			case "br":
				if inP {
					cur.WriteString("\n")
				}
			}
		case xml.EndElement:
			if t.Name.Local == "p" && inP {
				if s := strings.TrimSpace(cur.String()); s != "" {
					paras = append(paras, s)
				}
				cur.Reset()
				inP = false
			}
		case xml.CharData:
			if inP {
				cur.WriteString(string(t))
			}
		}
	}
	return paras, nil
}
