package usecase

import (
	"encoding/json"
	"strings"
	"testing"

	"knowledge_ingestion/src/agent/eino"
	"knowledge_ingestion/src/controller/dtos"
)

// buildCharts là đường duy nhất chart đi từ collector ra response: map từng
// field nguyên vẹn (spec không được bẻ cong) và LUÔN trả mảng, vì FE render
// `@for (chart of turn.charts)` — null là vỡ template.
func TestBuildChartsMapsCollectorVerbatim(t *testing.T) {
	spec := map[string]any{"series": []any{map[string]any{"type": "bar", "data": []float64{1500000}}}}
	got := buildCharts([]*eino.CollectedChart{
		{N: 2, Kind: "bar", Spec: spec, Caption: "Nguồn: [^2] · Phụ cấp"},
		nil,
	})
	if len(got) != 1 {
		t.Fatalf("entry nil phải bị bỏ, got %+v", got)
	}
	ch := got[0]
	if ch.N != 2 || ch.Kind != "bar" || ch.Caption != "Nguồn: [^2] · Phụ cấp" {
		t.Fatalf("map sai: %+v", ch)
	}
	if ch.Spec["series"] == nil {
		t.Fatal("spec phải về nguyên vẹn cho FE")
	}
}

// JSON của response phải có "charts": [] khi không vẽ gì — hợp đồng một hình
// dạng (không null) là thứ FE dựa vào.
func TestBuildChartsEmptyIsArrayNotNull(t *testing.T) {
	raw, err := json.Marshal(&dtos.ChatResponse{Answer: "đáp", Cites: []*dtos.ChatCite{}, Charts: buildCharts(nil)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, `"charts":[]`) {
		t.Fatalf(`response phải có "charts":[], got %s`, got)
	}
}
