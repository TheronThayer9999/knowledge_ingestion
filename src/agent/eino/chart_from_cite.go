package eino

import (
	"context"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"

	"knowledge_ingestion/src/common/logs"
)

// ChartFromCiteName là tên tool duy nhất LLM thấy.
const ChartFromCiteName = "chart_from_cite"

// ChartFromCiteArgs là toàn bộ những gì model được quyết định về một biểu đồ:
// nguồn nào, loại gì, tiêu đề gì, cột nào. Không có tham số nào là con số dữ
// liệu — số nằm trong chunk và backend cắt ra (plans/2026-10-27-dot-26 §2).
type ChartFromCiteArgs struct {
	N      flexInt `json:"n" jsonschema:"description=Số trích dẫn [^n] đã hiện trong kết quả tool của lượt này (nguồn thứ n). Bắt buộc."`
	Kind   string  `json:"kind" jsonschema:"description=Loại biểu đồ. bar để so sánh từng mục; line cho diễn biến theo thời gian; pie cho tỷ lệ thành phần. Bắt buộc."`
	Title  string  `json:"title,omitempty" jsonschema:"description=Tiêu đề biểu đồ ngắn bằng tiếng Việt theo đúng câu người dùng hỏi."`
	Column string  `json:"column,omitempty" jsonschema:"description=Tên cột số cần vẽ chép nguyên văn từ hàng header của bảng trong nguồn. Bỏ trống khi bảng chỉ có một cột số."`
}

// chartFromCiteResult trả cho model: đủ points (label/value) để viết câu chữ,
// không trả biểu đồ dựng sẵn — model copy số là thêm một đường sinh lỗi.
type chartFromCiteResult struct {
	OK      bool        `json:"ok"`
	N       int         `json:"n"`
	Kind    string      `json:"kind,omitempty"`
	Title   string      `json:"title,omitempty"`
	Points  []dataPoint `json:"points,omitempty"`
	Caption string      `json:"caption,omitempty"`
	Note    string      `json:"note,omitempty"`
}

const chartFromCiteDesc = `Vẽ BIỂU ĐỒ từ chính đoạn tài liệu đã trích dẫn trong lượt trả lời này. Model chỉ được chọn nguồn [n], loại biểu đồ, tiêu đề và (tuỳ ý) tên cột số; MỌI con số trong biểu đồ do backend cắt ra từ đoạn đã dẫn.

KHI NÀO GỌI: người dùng nói "vẽ biểu đồ", "vẽ đồ thị", "cho tôi thấy trực quan", "so sánh bằng hình"; hoặc câu hỏi so sánh nhiều hàng trong bảng mà vẽ rõ hơn liệt kê.

KHI NÀO KHÔNG GỌI: chưa có nguồn [^n] nào trong lượt (phải knowledge_search hoặc read_sheet trước); người dùng chỉ muốn đọc số; nguồn là văn bản không có bảng số; chào hỏi hoặc tóm tắt.

CÁCH DÙNG: n là số trích dẫn đã thấy. kind chỉ nhận bar, line, pie. Bảng có nhiều cột số thì truyền column đúng tên ô header (chẳng hạn "Phụ cấp (VNĐ)"); không truyền thì backend chọn cột có nhiều hàng số nhất và nói rõ trong note — đọc note, nếu không đúng ý người dùng thì gọi lại với column khác. Kết quả trả về chỉ gồm points (label/value), caption và note.

TRẢ LỜI KHI TOOL TỪ CHỐI: ok=false là kết quả bình thường, không phải lỗi hệ thống. Gọi lại với đúng các tham số cũ là vô ích — hãy trả lời bằng bảng số và nói thẳng vì sao không vẽ được (thiếu nguồn, dữ liệu không phải bảng, hay quá nhiều hàng).

VÍ DỤ: sau read_sheet bảng lương, vẽ phụ cấp từng nhân viên → chart_from_cite({"n": 1, "kind": "bar", "title": "Phụ cấp theo nhân viên", "column": "Phụ cấp (VNĐ)"}).`

// NewChartFromCiteTool dựng tool. Tool không cần repo hay vector store: nguồn
// duy nhất là HitCollector của lượt đang chạy, nên thứ nó đọc luôn là thứ người
// dùng đã thấy trong trích dẫn — không thể vẽ từ dữ liệu model bịa ra.
func NewChartFromCiteTool() (einotool.InvokableTool, error) {
	return toolutils.InferTool(ChartFromCiteName, chartFromCiteDesc,
		func(ctx context.Context, in ChartFromCiteArgs) (*chartFromCiteResult, error) {
			scope, ok := ScopeFrom(ctx)
			if !ok {
				return nil, fmt.Errorf("eino chart_from_cite: thiếu scope request trong ctx")
			}
			collector := scope.Collector
			if collector == nil {
				return nil, fmt.Errorf("eino chart_from_cite: thiếu HitCollector trong ctx")
			}
			return runChartFromCite(collector, in)
		})
}

// runChartFromCite tách khỏi closure để test gọi trực tiếp được và để hàm dựng
// tool nằm trong ngân sách.
func runChartFromCite(collector *HitCollector, in ChartFromCiteArgs) (*chartFromCiteResult, error) {
	n := int(in.N)
	chunk, ok := collector.Cite(n)
	if !ok {
		return &chartFromCiteResult{N: n, Note: fmt.Sprintf(
			"chưa có nguồn [^%d] trong lượt này — hãy knowledge_search hoặc read_sheet để lấy đoạn chứa số trước rồi mới xin vẽ biểu đồ", n)}, nil
	}
	series := extractSeries(chunk.Text, in.Kind, in.Column)
	if len(series.Points) == 0 {
		return &chartFromCiteResult{N: n, Note: series.Note}, nil
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = series.Column
	}
	spec := buildEChartOption(series.Kind, title, series.Column, series.Points)
	if err := validateSpecNumbers(spec, series.Points); err != nil {
		// Không bao giờ để chart lệch số ra khỏi tool. Lỗi này là bug code,
		// không phải args sai, nên log đầy đủ rồi trả note cho model xoay.
		logs.Error(err, "chart_from_cite: spec chứa số không thuộc series", "n", n, "kind", series.Kind, "column", series.Column)
		return &chartFromCiteResult{N: n, Note: "backend không dựng biểu đồ vì số không khớp chuỗi dữ liệu — hãy trả lời bằng bảng"}, nil
	}
	caption := fmt.Sprintf("Nguồn: [^%d] · %s", n, title)
	if !collector.AddChart(&CollectedChart{N: n, Kind: series.Kind, Spec: spec, Caption: caption}) {
		return &chartFromCiteResult{N: n, Note: fmt.Sprintf(
			"lượt này đã vẽ đủ %d biểu đồ — trả lời bằng văn bản cho các câu còn lại", maxCollectedCharts)}, nil
	}
	return &chartFromCiteResult{
		OK: true, N: n, Kind: series.Kind, Title: title,
		Points: series.Points, Caption: caption, Note: series.Note,
	}, nil
}
