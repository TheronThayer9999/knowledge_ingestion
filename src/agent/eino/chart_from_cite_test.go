package eino

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"knowledge_ingestion/src/domain"
)

// chartScopeCtx nhét collector vào scope để tool tìm thấy nguồn [^n] đúng như
// một lượt chat thật chạy qua usecase.
func chartScopeCtx(collector *HitCollector) context.Context {
	return WithScope(context.Background(), Scope{UserID: 7, CategoryIDs: []int64{3}, Collector: collector})
}

// runChartTool gọi tool qua đúng đường Eino (JSON args vào, JSON kết quả ra) —
// args rác phải được flexInt/flexInt64 nuốt sạch chứ không sập run.
func runChartTool(t *testing.T, ctx context.Context, args string) *chartFromCiteResult {
	t.Helper()
	tool, err := NewChartFromCiteTool()
	if err != nil {
		t.Fatalf("NewChartFromCiteTool: %v", err)
	}
	raw, err := tool.InvokableRun(ctx, args)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	var got chartFromCiteResult
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("output phải JSON parse được, got %q: %v", raw, err)
	}
	return &got
}

func chartCollectorWith(texts ...string) *HitCollector {
	c := &HitCollector{seen: make(map[string]int)}
	hits := make([]*domain.ScoredChunk, 0, len(texts))
	for i, text := range texts {
		hits = append(hits, &domain.ScoredChunk{
			PointID:    "p" + string(rune('a'+i)),
			ArticleID:  8,
			PageNum:    i + 1,
			ChunkIndex: i,
			Text:       text,
		})
	}
	c.Add(hits)
	return c
}

const salarySheetChunk = "## Sheet: Luong\n" +
	"| Mã NV | Họ và tên | Phụ cấp (VNĐ) | Tổng thực nhận (VNĐ) |\n" +
	"| NV01 | An | 1.500.000 | 9.000.000 |\n" +
	"| NV02 | Bình | 2.000.000 | 11.000.000 |\n" +
	"| NV03 | Chi | 500.000 | 7.000.000 |\n"

// TestChartFromCiteDrawsFromCitedChunk: nguồn duy nhất của số là chunk đã dẫn,
// cột model xin phải là cột model đọc được từ hàng header.
func TestChartFromCiteDrawsFromCitedChunk(t *testing.T) {
	c := chartCollectorWith(salarySheetChunk)
	got := runChartTool(t, chartScopeCtx(c),
		`{"n":1,"kind":"bar","title":"Phụ cấp theo nhân viên","column":"Phụ cấp (VNĐ)"}`)
	if !got.OK || got.N != 1 || got.Kind != kindBar {
		t.Fatalf("phải dựng được chart: %+v", got)
	}
	if len(got.Points) != 3 || got.Points[0].Label != "NV01" || got.Points[0].Value != 1500000 {
		t.Fatalf("points sai: %+v", got.Points)
	}
	if got.Title != "Phụ cấp theo nhân viên" {
		t.Fatalf("title sai: %q", got.Title)
	}
	if got.Caption != "Nguồn: [^1] · Phụ cấp theo nhân viên" {
		t.Fatalf("caption phải mang khuôn [^n] để FE bấm sang source: %q", got.Caption)
	}
	charts := c.SnapshotCharts()
	if len(charts) != 1 {
		t.Fatalf("collector phải có đúng 1 chart, got %d", len(charts))
	}
	ch := charts[0]
	if ch.N != 1 || ch.Kind != kindBar || ch.Caption != got.Caption {
		t.Fatalf("chart trong collector sai: %+v", ch)
	}
	if !strings.Contains(ch.Caption, "[^1]") {
		t.Fatalf("caption phải giữ [^1]: %q", ch.Caption)
	}
}

// Chart có nhiều cột số: không truyền column thì backend chọn cột có nhiều hàng
// số nhất và nói rõ trong note — model đọc note mà sửa nếu sai ý người dùng.
func TestChartFromCiteNotesAutoColumn(t *testing.T) {
	got := runChartTool(t, chartScopeCtx(chartCollectorWith(salarySheetChunk)),
		`{"n":1,"kind":"bar","title":"Tổng thực nhận"}`)
	if !got.OK {
		t.Fatalf("auto-column phải vẽ được: %+v", got)
	}
	if !strings.Contains(got.Note, "Tổng thực nhận") {
		t.Fatalf("note phải nói đã chọn cột nào, got %q", got.Note)
	}
}

// n ngoài phạm vi là model bịa nguồn: tool từ chối kèm đường xoay, không suy
// đoán chart từ dữ liệu cũ.
func TestChartFromCiteRejectsUnknownCite(t *testing.T) {
	c := chartCollectorWith(salarySheetChunk)
	for _, args := range []string{`{"n":0,"kind":"bar"}`, `{"n":2,"kind":"bar"}`, `{"n":9,"kind":"pie"}`} {
		got := runChartTool(t, chartScopeCtx(c), args)
		if got.OK || len(c.SnapshotCharts()) != 0 {
			t.Fatalf("%s: n không có trong collector phải từ chối, got %+v", args, got)
		}
		if !strings.Contains(got.Note, "nguồn") {
			t.Fatalf("note phải bảo model đi tìm nguồn trước, got %q", got.Note)
		}
	}
}

// Nguồn là văn xuôi không có bảng: từ chối là kết quả bình thường, note bắt
// buộc giải thích vì sao — Đợt 25 dạy rằng model chỉ sửa được khi tool nói rõ.
func TestChartFromCiteRejectsProseSource(t *testing.T) {
	got := runChartTool(t, chartScopeCtx(chartCollectorWith("Điều 13. Phạt 0,05%/ngày chậm nộp hồ sơ.")),
		`{"n":1,"kind":"pie","title":"Tỷ lệ phạt"}`)
	if got.OK || got.Note == "" {
		t.Fatalf("văn bản không bảng phải bị từ chối kèm note: %+v", got)
	}
	if !strings.Contains(got.Note, "bảng") {
		t.Fatalf("note phải nói nguồn không có bảng, got %q", got.Note)
	}
}

// kind rác không được âm thầm vẽ bar — model phải nhận về lý do.
func TestChartFromCiteRejectsUnknownKind(t *testing.T) {
	got := runChartTool(t, chartScopeCtx(chartCollectorWith(salarySheetChunk)),
		`{"n":1,"kind":"scatter"}`)
	if got.OK || !strings.Contains(got.Note, "bar") {
		t.Fatalf("scatter phải bị từ chối kèm danh sách loại hợp lệ: %+v", got)
	}
}

// line trên nhãn phân loại rơi về bar và nói thẳng trong note: vẽ đường cho
// NV01/NV02/NV03 là lừa mắt người dùng.
func TestChartFromCiteLineFallsBackToBar(t *testing.T) {
	c := chartCollectorWith(salarySheetChunk)
	got := runChartTool(t, chartScopeCtx(c), `{"n":1,"kind":"line","title":"Phụ cấp","column":"Phụ cấp (VNĐ)"}`)
	if !got.OK || got.Kind != kindBar {
		t.Fatalf("line trên dữ liệu phân loại phải rơi về bar: %+v", got)
	}
	if !strings.Contains(got.Note, "đã vẽ bar") {
		t.Fatalf("note phải nói rõ đã rơi kind, got %q", got.Note)
	}
	if charts := c.SnapshotCharts(); len(charts) != 1 || charts[0].Kind != kindBar {
		t.Fatalf("chart lưu phải là bar: %+v", charts)
	}
}

// Model gọi lại y hệt (sửa note rồi gửi lại) không được nhân đôi biểu đồ trên
// màn hình.
func TestChartFromCiteDedupesIdenticalChart(t *testing.T) {
	c := chartCollectorWith(salarySheetChunk)
	args := `{"n":1,"kind":"pie","title":"Cơ cấu phụ cấp","column":"Phụ cấp (VNĐ)"}`
	if got := runChartTool(t, chartScopeCtx(c), args); !got.OK {
		t.Fatalf("lần đầu phải vẽ được: %+v", got)
	}
	if got := runChartTool(t, chartScopeCtx(c), args); !got.OK {
		t.Fatalf("lần lặp vẫn phải ok để model không tưởng là lỗi: %+v", got)
	}
	if charts := c.SnapshotCharts(); len(charts) != 1 {
		t.Fatalf("chart trùng không được nhân đôi, got %d", len(charts))
	}
}

// Trần chart của một lượt: vẽ đủ là từ chối chứ không âm thầm chất thêm.
func TestChartFromCiteCeilsCharts(t *testing.T) {
	c := chartCollectorWith(salarySheetChunk)
	for i := range maxCollectedCharts + 1 {
		args := `{"n":1,"kind":"bar","title":"Phụ cấp theo nhân viên","column":"Phụ cấp (VNĐ)"}`
		got := runChartTool(t, chartScopeCtx(c), strings.Replace(args, "Phụ cấp theo nhân viên", "Biểu đồ "+string(rune('A'+i)), 1))
		if i < maxCollectedCharts {
			if !got.OK {
				t.Fatalf("chart thứ %d phải được dựng: %+v", i+1, got)
			}
			continue
		}
		if got.OK || !strings.Contains(got.Note, "đã vẽ đủ") {
			t.Fatalf("vượt trần phải từ chối kèm note, got %+v", got)
		}
	}
	if charts := c.SnapshotCharts(); len(charts) != maxCollectedCharts {
		t.Fatalf("collector phải dừng ở trần %d, got %d", maxCollectedCharts, len(charts))
	}
}

// Thiếu scope/collector là lỗi lập trình (usecase quên WithScope) — tool phải
// trả error chứ không vẽ chart từ phạm vi rỗng.
func TestChartFromCiteRequiresScopeAndCollector(t *testing.T) {
	tool, err := NewChartFromCiteTool()
	if err != nil {
		t.Fatalf("NewChartFromCiteTool: %v", err)
	}
	if _, err := tool.InvokableRun(context.Background(), `{"n":1,"kind":"bar"}`); err == nil ||
		!strings.Contains(err.Error(), "scope") {
		t.Fatalf("thiếu scope phải lỗi nhắc scope, got %v", err)
	}
	noCollector := WithScope(context.Background(), Scope{UserID: 1})
	if _, err := tool.InvokableRun(noCollector, `{"n":1,"kind":"bar"}`); err != nil {
		t.Fatalf("WithScope tự tạo collector nên không được lỗi: %v", err)
	}
	if got := runChartTool(t, noCollector, `{"n":1,"kind":"bar"}`); got.OK {
		t.Fatalf("collector rỗng thì chưa có nguồn nào: %+v", got)
	}
}

// Args rác ("None", số dạng chuỗi) phải được flexInt nuốt và trả note mời sửa —
// một args sai không được phép abort cả run thành 500.
func TestChartFromCiteToleratesGarbageArgs(t *testing.T) {
	got := runChartTool(t, chartScopeCtx(chartCollectorWith(salarySheetChunk)),
		`{"n":"None","kind":"bar"}`)
	if got.OK {
		t.Fatalf("n rác phải về 0 rồi bị từ chối: %+v", got)
	}
	got = runChartTool(t, chartScopeCtx(chartCollectorWith(salarySheetChunk)),
		`{"n":"1","kind":"bar","title":"Phụ cấp","column":"Phụ cấp (VNĐ)"}`)
	if !got.OK || len(got.Points) != 3 {
		t.Fatalf("n dạng chuỗi \"1\" vẫn là nguồn 1: %+v", got)
	}
}

// Cổng validate: một float không thuộc series không được đi qua, kể cả khi nó
// chỉ nằm sâu trong spec (bug tương lai thêm số vào biểu đồ mà quên nguồn).
func TestValidateSpecNumbersRejectsUnsourcedFloat(t *testing.T) {
	points := []dataPoint{{Label: "NV01", Value: 1500000}, {Label: "NV02", Value: 2000000}}
	spec := buildEChartOption(kindBar, "Phụ cấp", "Phụ cấp (VNĐ)", points)
	if err := validateSpecNumbers(spec, points); err != nil {
		t.Fatalf("spec do chính code dựng phải sạch: %v", err)
	}
	series := spec["series"].([]any)[0].(map[string]any)
	series["data"] = []any{1500000.0, 2000000.0, 12345.6}
	if err := validateSpecNumbers(spec, points); err == nil {
		t.Fatal("phải bắt ra số 12345.6 không thuộc series nào")
	}
	// Pie: số nằm trong map của mảng []map[string]any cũng phải bị soi.
	pie := buildEChartOption(kindPie, "Cơ cấu", "Phụ cấp (VNĐ)", points)
	pieData := pie["series"].([]any)[0].(map[string]any)["data"].([]map[string]any)
	pieData[0]["value"] = 99.9
	if err := validateSpecNumbers(pie, points); err == nil {
		t.Fatal("pie cũng phải bị soi từng value")
	}
}
