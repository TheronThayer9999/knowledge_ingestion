package extractor

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// bigVietnameseText sinh text tiếng Việt có dấu, nhiều dòng, dài hơn nhiều
// lần TextPieceBytes, để kiểm tra việc chia mảnh không làm mất byte nào.
func bigVietnameseText(lines int) string {
	var sb strings.Builder
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&sb, "Điều %d. Nội dung quy định số %d — áp dụng cho người lao động ở Hà Nội.\n", i, i*7)
	}
	return sb.String()
}

func TestPlainTextPiecesKeepsEveryByte(t *testing.T) {
	src := bigVietnameseText(40000) // ~3MB, cần nhiều mảnh
	if len(src) <= 4*TextPieceBytes {
		t.Fatalf("dữ liệu test quá nhỏ: %d byte", len(src))
	}

	var pieces []Page
	err := plainTextPieces(strings.NewReader(src), func(p Page) error {
		pieces = append(pieces, p)
		return nil
	})
	if err != nil {
		t.Fatalf("plainTextPieces: %v", err)
	}
	if len(pieces) < 2 {
		t.Fatalf("kỳ vọng nhiều mảnh, nhận %d", len(pieces))
	}

	var joined bytes.Buffer
	for i, p := range pieces {
		if p.Seq != i+1 {
			t.Fatalf("mảnh %d có Seq=%d, kỳ vọng %d", i, p.Seq, i+1)
		}
		joined.WriteString(p.Text)
	}
	if joined.String() != src {
		t.Fatalf("mất dữ liệu khi chia mảnh: gốc %d byte, ghép lại %d byte", len(src), joined.Len())
	}
}

func TestVisitPiecesKeepsEveryByte(t *testing.T) {
	src := bigVietnameseText(40000)

	var pieces []Page
	err := visitPieces(0, src, func(p Page) error {
		pieces = append(pieces, p)
		return nil
	})
	if err != nil {
		t.Fatalf("visitPieces: %v", err)
	}
	if len(pieces) < 2 {
		t.Fatalf("kỳ vọng nhiều mảnh, nhận %d", len(pieces))
	}

	var joined bytes.Buffer
	for _, p := range pieces {
		joined.WriteString(p.Text)
	}
	if joined.String() != src {
		t.Fatalf("mất dữ liệu khi chia mảnh: gốc %d byte, ghép lại %d byte", len(src), joined.Len())
	}
}

func TestPiecesSameOutputForSameInput(t *testing.T) {
	src := bigVietnameseText(20000)

	var streamed, inRAM []string
	if err := plainTextPieces(strings.NewReader(src), func(p Page) error {
		streamed = append(streamed, p.Text)
		return nil
	}); err != nil {
		t.Fatalf("plainTextPieces: %v", err)
	}
	if err := visitPieces(0, src, func(p Page) error {
		inRAM = append(inRAM, p.Text)
		return nil
	}); err != nil {
		t.Fatalf("visitPieces: %v", err)
	}
	if strings.Join(streamed, "") != strings.Join(inRAM, "") {
		t.Fatalf("hai đường chia cho kết quả khác nhau")
	}
}

func TestPiecesNoNewlineIsNotCut(t *testing.T) {
	src := strings.Repeat("á", TextPieceBytes) // không có newline nào
	var joined bytes.Buffer
	err := visitPieces(0, src, func(p Page) error {
		joined.WriteString(p.Text)
		return nil
	})
	if err != nil {
		t.Fatalf("visitPieces: %v", err)
	}
	if joined.String() != src {
		t.Fatalf("text không có newline bị mất byte: gốc %d, ghép %d", len(src), joined.Len())
	}
}
