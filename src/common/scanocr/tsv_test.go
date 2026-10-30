package scanocr

import (
	"math"
	"strings"
	"testing"
)

// tsvFixture là output `tesseract … tsv` thu nhỏ của một trang A4 @300DPI
// (2480x3508 px): hai dòng thật, một dòng chỉ có box (level 4), một "từ" mang
// conf -1 và một trang/block header — tất cả những thứ đó phải bị loại.
const tsvFixture = "level\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n" +
	"1\t1\t0\t0\t0\t0\t0\t2480\t3508\t-1\t\n" +
	"5\t1\t1\t0\t0\t0\t0\t2480\t3508\t-1\t\n" +
	"5\t1\t1\t1\t1\t100\t200\t300\t60\t95.5\tCộng\n" +
	"5\t1\t1\t1\t2\t420\t200\t240\t60\t88.5\thoà\n" +
	"5\t1\t1\t2\t1\t100\t300\t500\t60\t40.0\txã\n" +
	"5\t1\t1\t2\t2\t620\t300\t300\t60\t69.0\thội,\n" +
	"5\t1\t1\t3\t1\t100\t400\t100\t50\t-1\t\n"

func tsvImage() ImageInfo {
	return ImageInfo{Page: 7, WidthPx: 2480, HeightPx: 3508, DPI: 300, Extension: ".png"}
}

func TestParseTSVBuildsLinesFromWords(t *testing.T) {
	page, err := parseTSV(tsvFixture, tsvImage())
	if err != nil {
		t.Fatalf("parseTSV lỗi: %v", err)
	}
	if page.Num != 7 || page.WidthPx != 2480 || page.HeightPx != 3508 || page.DPI != 300 {
		t.Fatalf("siêu dữ liệu trang sai: %+v", page)
	}
	if len(page.Lines) != 2 {
		t.Fatalf("số dòng = %d, muốn 2 (%+v)", len(page.Lines), page.Lines)
	}
	if got, want := page.Text(), "Cộng hoà\nxã hội,"; got != want {
		t.Fatalf("Text() = %q, muốn %q", got, want)
	}

	first := page.Lines[0]
	// Box dòng là hình bao các từ, không phải box level 4 (bao cả khoảng trắng
	// giữa các từ nên tô sáng sẽ lem ra lề).
	if first.Box != (Box{X: 100, Y: 200, W: 560, H: 60}) {
		t.Fatalf("box dòng 1 = %+v, muốn {100 200 560 60}", first.Box)
	}
	if math.Abs(first.Conf-92.0) > 1e-9 {
		t.Fatalf("conf dòng 1 = %v, muốn trung bình 92", first.Conf)
	}
	if len(first.Words) != 2 || first.Words[1].Text != "hoà" || first.Words[1].Box.X != 420 {
		t.Fatalf("từ dòng 1 sai: %+v", first.Words)
	}
}

func TestParseTSVKeepsOneLinePerKeyEvenIfInterleaved(t *testing.T) {
	// Tesseract không xen kẽ, nhưng parser từng dựa vào "khác dòng cuối" nên hai
	// đoạn cùng key bị tách làm hai dòng và đoạn sau mất sạch từ. Khoá lại bằng
	// test này: mọi từ của một key phải về CÙNG một dòng, theo thứ tự xuất hiện.
	out := strings.SplitAfter(tsvFixture, "text\n")[0] +
		"5\t1\t1\t1\t1\t100\t100\t50\t40\t90\tA\n" +
		"5\t1\t1\t2\t1\t100\t200\t50\t40\t90\tB\n" +
		"5\t1\t1\t1\t2\t200\t100\t50\t40\t90\tC\n"
	page, err := parseTSV(out, tsvImage())
	if err != nil {
		t.Fatalf("parseTSV lỗi: %v", err)
	}
	if len(page.Lines) != 2 {
		t.Fatalf("số dòng = %d, muốn 2", len(page.Lines))
	}
	if got := page.Lines[0].Text; got != "A C" {
		t.Fatalf("dòng đầu = %q, muốn \"A C\"", got)
	}
}

func TestParseTSVRejectsOutputWithoutHeader(t *testing.T) {
	// Binary trỏ nhầm / bản in text suông: phải có lỗi, không được im lặng trả
	// trang 0 dòng (worker sẽ ghi "OCR không ra chữ" thay vì "engine sai").
	if _, err := parseTSV("Cộng hoà xã hội chủ nghĩa\n", tsvImage()); err == nil {
		t.Fatal("muốn lỗi header, nhận được page hợp lệ")
	}
}

func TestPageStatsCountsWords(t *testing.T) {
	page, err := parseTSV(tsvFixture, tsvImage())
	if err != nil {
		t.Fatalf("parseTSV lỗi: %v", err)
	}
	st := page.Stats(70)
	// Hai từ dưới ngưỡng 70 ("xã" 40 và "hội," 69) — thanh lọc của UI đếm theo
	// từ vì người dùng sửa từng từ một.
	if st.Words != 4 || st.LowConf != 2 {
		t.Fatalf("stats = %+v, muốn 4 từ và 2 từ đáng ngờ", st)
	}
	if mean := (95.5 + 88.5 + 40 + 69) / 4; math.Abs(st.MeanConf-mean) > 1e-9 {
		t.Fatalf("MeanConf = %v, muốn %v", st.MeanConf, mean)
	}
	// Trang rỗng không được chia 0 khi tính trung bình.
	if empty := (&Page{}).Stats(70); empty.Words != 0 || empty.MeanConf != 0 {
		t.Fatalf("stats trang rỗng = %+v", empty)
	}
}

func TestNormalizedBoxRoundTrip(t *testing.T) {
	// Toạ độ đi qua DB/FE ở tỉ lệ 0..1: ảnh preview 150DPI nhỏ hơn ảnh OCR 4 lần
	// mà box vẫn đúng, đó là toàn bộ lý do tồn tại của kiểu này.
	b := Box{X: 620, Y: 300, W: 300, H: 60}
	n := b.NormalizedBox(2480, 3508)
	back := n.ToBox(2480, 3508)
	if back != b {
		t.Fatalf("round-trip = %+v, muốn %+v", back, b)
	}
	if n.X <= 0 || n.X >= 1 || n.Y <= 0 || n.Y >= 1 {
		t.Fatalf("box chuẩn hoá ngoài 0..1: %+v", n)
	}
	// Trang lỗi (kích thước 0) trả box rỗng chứ không panic — row vẫn phải ghi.
	if got := b.NormalizedBox(0, 0); got != (NormalizedBox{}) {
		t.Fatalf("NormalizedBox(0,0) = %+v, muốn box rỗng", got)
	}
}

func TestConfigNormalizeDefaults(t *testing.T) {
	// Config rỗng = "chưa khai báo gì" nhưng máy có Tesseract: mọi núm phải về
	// giá trị đã đo, không có 0 nào lọt xuống lệnh tesseract.
	got := Config{}.Normalize()
	if got.Engine != EngineTesseract || got.Binary != "tesseract" || got.Lang != "vie" || got.PSM != 6 {
		t.Fatalf("defaults engine sai: %+v", got)
	}
	if got.RenderBinary != "pdftoppm" || got.RenderDPI != 300 || got.PreviewDPI != 150 {
		t.Fatalf("defaults render sai: %+v", got)
	}
	if got.TextLayerMinChars != 40 || got.MinConfWord != 70 || got.MaxAttempts != 3 || got.Parallel != 2 {
		t.Fatalf("defaults ngưỡng sai: %+v", got)
	}
	// Trần trang phải CHỨA trần một lần gọi engine (deadline cắt giữa OCR là
	// kiểu fail tốn tiền nhất).
	if got.PageTimeout <= got.Timeout {
		t.Fatalf("PageTimeout %v <= Timeout %v", got.PageTimeout, got.Timeout)
	}
	// Ngôn ngữ tường minh không bị default đè.
	if kept := (Config{Lang: "vie+eng", Engine: "Tesseract "}).Normalize(); kept.Lang != "vie+eng" || kept.Engine != "tesseract" {
		t.Fatalf("Normalize đè giá trị người dùng: %+v", kept)
	}
}
