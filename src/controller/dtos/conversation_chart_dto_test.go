package dtos

import (
	"strings"
	"testing"

	"knowledge_ingestion/src/domain"
)

// Chart sống trong DB dưới dạng jsonb một cột, cùng khuôn với cites: Encode trả
// "[]" cho rỗng (cột NOT NULL có default), Decode đọc lại nguyên spec — mở lại
// phiên là biểu đồ vẽ lại y hệt, không phải suy đoán.
func TestEncodeDecodeChartsRoundTrip(t *testing.T) {
	raw, err := EncodeCharts([]*ChatChart{{
		N:    1,
		Kind: "pie",
		Spec: map[string]any{
			"series": []any{map[string]any{"type": "pie", "data": []any{
				map[string]any{"name": "NV01", "value": 1500000.0},
			}}},
		},
		Caption: "Nguồn: [^1] · Cơ cấu phụ cấp",
	}})
	if err != nil {
		t.Fatalf("EncodeCharts: %v", err)
	}
	if !strings.Contains(raw, `"value":1500000`) {
		t.Fatalf("spec phải về dạng ECharts đọc được, got %s", raw)
	}

	back, err := DecodeCharts(raw)
	if err != nil {
		t.Fatalf("DecodeCharts: %v", err)
	}
	if len(back) != 1 || back[0].N != 1 || back[0].Kind != "pie" {
		t.Fatalf("round-trip sai: %+v", back)
	}
	if back[0].Spec["series"] == nil {
		t.Fatalf("spec mất series sau round-trip: %+v", back[0].Spec)
	}
}

// Rỗng phải là "[]" chứ không phải "" hay "null": cột NOT NULL nhận "" là lỗi
// ghi cả message, và message của user luôn đi đường này.
func TestEncodeChartsEmptyIsJSONArray(t *testing.T) {
	for _, charts := range [][]*ChatChart{nil, {}} {
		raw, err := EncodeCharts(charts)
		if err != nil || raw != "[]" {
			t.Fatalf("EncodeCharts(%v) = %q, err %v, want \"[]\"", charts, raw, err)
		}
	}
	for _, raw := range []string{"", "[]"} {
		got, err := DecodeCharts(raw)
		if err != nil || len(got) != 0 {
			t.Fatalf("DecodeCharts(%q) = %+v, err %v, want rỗng không lỗi", raw, got, err)
		}
	}
	// Dữ liệu hỏng (cột bị sửa tay): lỗi phải nổi lên chứ không trả chart cụt.
	if _, err := DecodeCharts(`{"n":1}`); err == nil {
		t.Fatal("json rác phải lỗi vì không decode ra mảng được")
	}
}

// Message đọc từ DB phải mang charts sang response — thiếu bước này là mở lại
// phiên mất biểu đồ dù spec vẫn nằm trong DB.
func TestToMessageResponseCarriesCharts(t *testing.T) {
	charts, err := EncodeCharts([]*ChatChart{{N: 3, Kind: "line", Caption: "Nguồn: [^3] · Diễn biến"}})
	if err != nil {
		t.Fatalf("EncodeCharts: %v", err)
	}
	resp, err := ToMessageResponse(&domain.ChatMessage{
		Role:                 domain.MessageRoleAssistant,
		Content:              "đáp[^3]",
		Cites:                "[]",
		Charts:               charts,
		EffectiveCategoryIDs: "[]",
	})
	if err != nil {
		t.Fatalf("ToMessageResponse: %v", err)
	}
	if len(resp.Charts) != 1 || resp.Charts[0].N != 3 || resp.Charts[0].Kind != "line" {
		t.Fatalf("charts phải về nguyên mảng đã lưu: %+v", resp.Charts)
	}

	// Message cũ (cột mới thêm có default '[]') và message của user: rỗng,
	// không lỗi, không null hình dạng khác.
	legacy, err := ToMessageResponse(&domain.ChatMessage{Role: domain.MessageRoleUser, Cites: "[]", Charts: "[]", EffectiveCategoryIDs: "[]"})
	if err != nil || len(legacy.Charts) != 0 {
		t.Fatalf("message không chart phải đọc sạch, got %+v err %v", legacy, err)
	}
}
