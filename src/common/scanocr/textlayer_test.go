package scanocr

import (
	"context"
	"math"
	"strings"
	"testing"
)

// bboxHaiDòng là output rút gọn của `pdftotext -bbox`: hai dòng, mỗi dòng hai từ,
// toạ độ Pt gốc trên-trái. Viết tay để test parser KHÔNG phụ thuộc poppler.
const bboxHaiDòng = `<doc>
  <page width="595.000000" height="842.000000">
    <word xMin="56.0" yMin="33.3" xMax="82.0" yMax="44.4">Phòng</word>
    <word xMin="85.3" yMin="33.4" xMax="111.8" yMax="44.5">Nhân</word>
    <word xMin="56.0" yMin="47.3" xMax="73.1" yMax="58.4">sự</word>
    <word xMin="76.4" yMin="47.2" xMax="105.6" yMax="58.3">niêm</word>
  </page>
</doc>`

func TestParseBBoxGroupsWordsIntoLines(t *testing.T) {
	page, err := parseBBox(bboxHaiDòng, 3, 300)
	if err != nil {
		t.Fatalf("parseBBox: %v", err)
	}
	if page.Num != 3 || page.DPI != 300 || page.EngineName != EngineTextLayer {
		t.Fatalf("đầu trang sai: %+v", page)
	}
	// Pt → px theo DPI render: 595pt * 300/72 = 2479.17px.
	if math.Abs(page.WidthPx-2479.17) > 0.5 || math.Abs(page.HeightPx-3508.33) > 0.5 {
		t.Fatalf("kích thước px sai: %v x %v", page.WidthPx, page.HeightPx)
	}
	if len(page.Lines) != 2 {
		t.Fatalf("muốn 2 dòng, nhận %d: %+v", len(page.Lines), page.Lines)
	}
	if got, want := page.Lines[0].Text, "Phòng Nhân"; got != want {
		t.Fatalf("dòng 1 = %q, muốn %q", got, want)
	}
	if got, want := page.Lines[1].Text, "sự niêm"; got != want {
		t.Fatalf("dòng 2 = %q, muốn %q", got, want)
	}
	// Box dòng là hình bao các từ: từ xMin từ đầu tới xMax từ cuối.
	l0 := page.Lines[0].Box
	if math.Abs(l0.X-ptToPx(56, 300)) > 0.01 || math.Abs(l0.X+l0.W-ptToPx(111.8, 300)) > 0.01 {
		t.Fatalf("box dòng 1 sai: %+v", l0)
	}
	// Chữ có sẵn trong PDF là nguyên văn nên conf 100 — FE nhờ đó không tô amber.
	for _, w := range page.Lines[0].Words {
		if w.Conf != 100 {
			t.Fatalf("conf của từ %q = %v, muốn 100", w.Text, w.Conf)
		}
	}
	if got := page.Text(); !strings.Contains(got, "Phòng Nhân") {
		t.Fatalf("Text() = %q", got)
	}
}

func TestParseBBoxWithoutPageSize(t *testing.T) {
	// Không có <page width height> thì KHÔNG ĐƯỢC trả Page im lặng với px = 0:
	// mọi box chuẩn hoá về 0..1 sẽ chia cho 0 và lớp chữ vô hình nhảy vị trí.
	if _, err := parseBBox(`<doc><word xMin="1" yMin="1" xMax="2" yMax="2">a</word></doc>`, 1, 300); err == nil {
		t.Fatal("muốn lỗi khi thiếu kích thước trang")
	}
}

func TestParseBBoxSkipsJunkWords(t *testing.T) {
	doc := `<doc><page width="100" height="100">
		<word xMin="10" yMin="10" xMax="10" yMax="20"></word>
		<word xMin="10" yMin="10" xMax="9" yMax="20">trật tự sai</word>
		<word xMin="10" yMin="10" xMax="30" yMax="20">ok</word>
	</page></doc>`
	page, err := parseBBox(doc, 1, 300)
	if err != nil {
		t.Fatalf("parseBBox: %v", err)
	}
	if len(page.Lines) != 1 || page.Lines[0].Text != "ok" {
		t.Fatalf("từ rỗng/h box âm phải bị bỏ: %+v", page.Lines)
	}
}

// --- Test với poppler + PDF thật ---------------------------------------------

func TestExtractTextLayerOnTextFixture(t *testing.T) {
	cfg := testConfig(t)
	path := fixture(t, "fixtures", "pdf_text_layer.pdf")
	page, err := ExtractTextLayer(context.Background(), cfg, path, 1)
	if err != nil {
		t.Fatalf("ExtractTextLayer: %v", err)
	}
	if len(page.Lines) == 0 {
		t.Fatal("fixture này có text layer nên phải đọc được từ")
	}
	text := page.Text()
	for _, want := range []string{"QUY", "Phòng"} {
		if !strings.Contains(text, want) {
			t.Fatalf("thiếu %q trong lớp chữ đọc được:\n%s", want, clip(text, 300))
		}
	}
	// A4 @300DPI ≈ 2480x3508 px — cùng hệ với ảnh render nên box OCR và box
	// text layer vẽ chồng lên nhau được.
	if page.WidthPx < 2400 || page.WidthPx > 2560 {
		t.Fatalf("WidthPx = %v, ngoài khoảng A4@300dpi", page.WidthPx)
	}
	st := page.Stats(float64(cfg.MinConfWord))
	if st.Words == 0 || st.LowConf != 0 {
		t.Fatalf("thống kê sai: %+v", st)
	}
}

func TestExtractTextLayerOnScanFixtureHasNoWords(t *testing.T) {
	cfg := testConfig(t)
	path := fixture(t, "fixtures", "pdf_scan.pdf")
	page, err := ExtractTextLayer(context.Background(), cfg, path, 1)
	if err != nil {
		t.Fatalf("ExtractTextLayer: %v", err)
	}
	// Trang scan thuần: không có dòng nào chứ không phải lỗi — service dùng kết
	// quả này để quyết định OCR (Ngưỡng TextLayerMinChars).
	if len(page.Lines) != 0 {
		t.Fatalf("fixture scan không được có text layer, nhận %d dòng", len(page.Lines))
	}
}

func TestExtractTextLayerRejectsPageZero(t *testing.T) {
	if _, err := ExtractTextLayer(context.Background(), testConfig(t), "x.pdf", 0); err == nil {
		t.Fatal("muốn lỗi khi số trang < 1")
	}
}
