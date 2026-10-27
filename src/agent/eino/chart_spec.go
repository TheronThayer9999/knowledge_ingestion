package eino

import (
	"fmt"
	"math"
)

// Dựng spec ECharts từ chuỗi số đã cắt ở chart_numbers.go. Tách riêng file này
// vì hai trách nhiệm khác nhau: chart_numbers quyết ĐỊNH số nào, chart_spec trình
// BÀY số đó — và không cho phép bất kỳ số nào chui vào spec ngoài series.

// maxCollectedCharts chặn số biểu đồ của một lượt chat: người dùng đọc được vài
// biểu đồ, nhiều hơn là spam và làm phình payload lưu DB.
const maxCollectedCharts = 4

// CollectedChart là biểu đồ đã dựng xong, nằm trong HitCollector cho tới lúc
// chat_service map ra DTO. Spec để dạng map vì chỉ frontend biết vẽ ECharts;
// backend không cần (và không nên) kéo lib chart phía Go vào.
type CollectedChart struct {
	// N là số trích dẫn nguồn của biểu đồ — khớp đúng [^n] trong answer.
	N int
	// Kind là loại thực vẽ (có thể khác kind model xin khi rơi về bar).
	Kind    string
	Spec    map[string]any
	Caption string
}

// buildEChartOption dựng đúng hai shape ECharts: trục category cho bar/line và
// pie cho tỷ lệ thành phần. Không có nhánh khác — thêm loại mới là phải thêm
// test chứ không âm thầm vẽ sai.
func buildEChartOption(kind, title, seriesName string, points []dataPoint) map[string]any {
	if kind == kindPie {
		return pieOption(title, seriesName, points)
	}
	return axisOption(kind, title, seriesName, points)
}

func axisOption(kind, title, name string, points []dataPoint) map[string]any {
	labels := make([]string, 0, len(points))
	values := make([]float64, 0, len(points))
	for i, p := range points {
		labels = append(labels, pointLabel(p, i))
		values = append(values, p.Value)
	}
	return map[string]any{
		"title":   map[string]any{"text": title, "left": "center"},
		"grid":    map[string]any{"left": 12, "right": 16, "bottom": 8, "top": 44, "containLabel": true},
		"tooltip": map[string]any{"trigger": "axis"},
		"xAxis":   map[string]any{"type": "category", "data": labels},
		"yAxis":   map[string]any{"type": "value"},
		"series":  []any{map[string]any{"name": name, "type": kind, "data": values}},
	}
}

func pieOption(title, name string, points []dataPoint) map[string]any {
	data := make([]map[string]any, 0, len(points))
	for i, p := range points {
		data = append(data, map[string]any{"name": pointLabel(p, i), "value": p.Value})
	}
	return map[string]any{
		"title":   map[string]any{"text": title, "left": "center"},
		"tooltip": map[string]any{"trigger": "item"},
		"legend":  map[string]any{"bottom": 0},
		"series": []any{map[string]any{
			"name":   name,
			"type":   kindPie,
			"radius": []string{"38%", "66%"},
			"data":   data,
		}},
	}
}

// pointLabel fallback khi cả hàng toàn số (không có ô làm nhãn).
func pointLabel(p dataPoint, i int) string {
	if p.Label != "" {
		return p.Label
	}
	return fmt.Sprintf("#%d", i+1)
}

// validateSpecNumbers là cổng cuối trước khi chart được ghi vào collector: mọi
// số THỰC nằm trong spec phải là một giá trị của series. Cổng này không chống
// model (model không chạm được vào spec) mà chống chính code về sau — thêm một
// con số vào biểu đồ mà quên nguồn là kiểu lỗi phải chết ở đây, không chết trên
// mắt người dùng.
//
// Số nguyên tính toán layout (padding, top, fontSize) không là float64 nên
// không bị soi: chỉ dữ liệu mới phải có nguồn.
func validateSpecNumbers(spec map[string]any, points []dataPoint) error {
	allowed := make(map[float64]bool, len(points))
	for _, p := range points {
		allowed[round6(p.Value)] = true
	}
	var found []float64
	collectFloats(spec, &found)
	for _, f := range found {
		if !allowed[round6(f)] {
			return fmt.Errorf("số %v trong spec không thuộc series nào", f)
		}
	}
	return nil
}

// collectFloats đi toàn cây spec gom số thực. []string (nhãn trục, radius) và
// int (layout) cố ý không gom: chúng không phải dữ liệu.
func collectFloats(v any, out *[]float64) {
	switch x := v.(type) {
	case map[string]any:
		for _, inner := range x {
			collectFloats(inner, out)
		}
	case []any:
		for _, inner := range x {
			collectFloats(inner, out)
		}
	case []map[string]any:
		for _, inner := range x {
			collectFloats(inner, out)
		}
	case []float64:
		*out = append(*out, x...)
	case float64:
		*out = append(*out, x)
	}
}

// round6 so float sau 6 chữ số thập phân: JSON và float64 cộng/trừ có thể lệch
// ở chữ số thứ 17, không được thành "số không có nguồn".
func round6(f float64) float64 {
	return math.Round(f*1e6) / 1e6
}
