package extractor

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// extractDocx trích text từ file Word (.docx) chỉ bằng stdlib — docx thực
// chất là ZIP chứa XML nên không cần dep ngoài. Chỉ đọc đúng 1 entry
// word/document.xml (không bung ra đĩa) nên miễn nhiễm zip-slip; giới hạn
// bytes đọc vào + bytes text xuất ra để miễn nhiễm zip-bomb.
//
// Quy ước xuất markdown để chunker cắt đúng:
//   - mỗi đoạn văn (w:p) thành 1 khối, cách nhau dòng trống → chunker ưu
//     tiên cắt ở "\n\n" nên đoạn không bị xé;
//   - đầu mục đánh số (w:numPr) giữ tiền tố "- " để không mất cấu trúc liệt kê;
//   - mỗi bảng (w:tbl) thành 1 khối pipe "| a | b |", mỗi hàng 1 dòng —
//     bảng vừa thì gọn trong 1 chunk, bảng to chunker cắt theo hàng;
//   - caption ảnh (wp:docPr descr) giữ thành dòng "[Hình: ...]" — chữ trong
//     ảnh thì chịu (không OCR đợt này), không sinh chunk rỗng;
//   - bỏ text xóa track-change (w:del) và mã field (w:instrText như PAGEREF,
//     TOC) vì đó là rác máy, không phải nội dung;
//   - header/footer/footnote (file xml khác) bỏ qua — chỉ lấy thân văn bản.
func extractDocx(r io.Reader) (string, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxDocxArchiveBytes+1))
	if err != nil {
		return "", fmt.Errorf("đọc file docx: %w", err)
	}
	if len(raw) > maxDocxArchiveBytes {
		return "", fmt.Errorf("%w: file docx quá lớn (%d bytes)", ErrInvalid, len(raw))
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", fmt.Errorf("%w: file docx không hợp lệ: %v", ErrInvalid, err)
	}
	var doc *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			doc = f
			break
		}
	}
	if doc == nil {
		return "", fmt.Errorf("%w: file docx thiếu word/document.xml", ErrInvalid)
	}
	// Trần XML giải nén — header zip có thể khai gian size nên vẫn đọc qua
	// LimitReader thay vì tin UncompressedSize64.
	rc, err := doc.Open()
	if err != nil {
		return "", fmt.Errorf("%w: mở word/document.xml: %v", ErrInvalid, err)
	}
	defer func() { _ = rc.Close() }()
	xmlData, err := io.ReadAll(io.LimitReader(rc, maxPlainTextBytes*4+1))
	if err != nil {
		return "", fmt.Errorf("đọc word/document.xml: %w", err)
	}

	var blocks []string
	var para, cellPara strings.Builder
	var cellParts []string
	var row, rows []string
	var inPara, inTc, bullet bool
	var skipDepth int // >0 khi đang trong w:del hoặc w:instrText

	flushPara := func() {
		var s string
		if inTc {
			s = strings.TrimSpace(cellPara.String())
			cellPara.Reset()
		} else {
			s = strings.TrimSpace(para.String())
			para.Reset()
		}
		if s == "" {
			return
		}
		if bullet {
			s = "- " + s
		}
		if inTc {
			cellParts = append(cellParts, s)
			return
		}
		blocks = append(blocks, s)
	}

	dec := xml.NewDecoder(bytes.NewReader(xmlData))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("%w: parse word/document.xml: %v", ErrInvalid, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				inPara = true
				bullet = false
			case "numPr":
				// Có mặt trong pPr nghĩa là đoạn này là đầu mục liệt kê.
				if inPara {
					bullet = true
				}
			case "del", "instrText":
				skipDepth++
			case "br", "cr":
				if inPara && skipDepth == 0 {
					if inTc {
						cellPara.WriteString("\n")
					} else {
						para.WriteString("\n")
					}
				}
			case "tab":
				if inPara && skipDepth == 0 {
					if inTc {
						cellPara.WriteString(" ")
					} else {
						para.WriteString(" ")
					}
				}
			case "tc":
				inTc = true
			case "docPr":
				// Caption/alt-text của ảnh — giữ lại để chunk về ảnh không rỗng.
				for _, a := range t.Attr {
					if a.Name.Local == "descr" && strings.TrimSpace(a.Value) != "" {
						blocks = append(blocks, "[Hình: "+strings.TrimSpace(a.Value)+"]")
						break
					}
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "p":
				if inPara && skipDepth == 0 {
					flushPara()
				} else if inTc {
					cellPara.Reset()
				} else {
					para.Reset()
				}
				inPara = false
				bullet = false
			case "del", "instrText":
				// Decoder báo lỗi với XML mất cân bằng nên EndElement luôn khớp
				// StartElement — guard đề phòng lib đổi behavior.
				if skipDepth > 0 {
					skipDepth--
				}
			case "tc":
				// Nhiều đoạn trong 1 ô thì nối bằng "; ".
				cell := strings.TrimSpace(strings.Join(cellParts, "; "))
				row = append(row, cell)
				cellParts = nil
				inTc = false
			case "tr":
				rows = append(rows, "| "+strings.Join(row, " | ")+" |")
				row = nil
			case "tbl":
				if len(rows) > 0 {
					blocks = append(blocks, strings.Join(rows, "\n"))
					rows = nil
				}
			}
		case xml.CharData:
			if inPara && skipDepth == 0 {
				if inTc {
					cellPara.WriteString(string(t))
				} else {
					para.WriteString(string(t))
				}
			}
		}
	}

	out := strings.TrimSpace(strings.Join(blocks, "\n\n"))
	if len(out) > maxPlainTextBytes {
		out = out[:maxPlainTextBytes]
		// Cắt byte thô có thể chém đôi rune cuối — gọt dần tới biên hợp lệ.
		for len(out) > 0 && !utf8.ValidString(out) {
			out = out[:len(out)-1]
		}
	}
	return out, nil
}
