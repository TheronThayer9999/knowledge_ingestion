package tools

import (
	"context"
	"testing"

	"knowledge_ingestion/src/common/constants"
)

func TestResolveLimit(t *testing.T) {
	cases := []struct {
		name         string
		requested    int
		routeDefault int
		want         int
	}{
		{"caller ep thang", 5, 10, 5},
		{"route default", 0, 10, 10},
		{"ca hai rong", 0, 0, constants.SEARCH_DEFAULT_LIMIT},
		{"chan tran max", 999, 10, constants.SEARCH_MAX_LIMIT},
		{"route vuot max", 0, 999, constants.SEARCH_MAX_LIMIT},
	}
	for _, tc := range cases {
		if got := ResolveLimit(tc.requested, tc.routeDefault); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestShouldSkip(t *testing.T) {
	skip := Decision{Tool: None, SkipSearch: true}
	if !ShouldSkip(skip, 0) {
		t.Error("chitchat không limit phải skip")
	}
	if ShouldSkip(skip, 5) {
		t.Error("caller ép limit phải thắng skip")
	}
	search := Decision{Tool: KnowledgeSearch}
	if ShouldSkip(search, 0) {
		t.Error("lookup không được skip")
	}
}

func TestExecuteSkipNoHybrid(t *testing.T) {
	// Skip trả rỗng trước khi chạm hybrid nên registry nil vẫn an toàn.
	r := NewRegistry(nil)
	got, err := r.Execute(context.Background(), Decision{Tool: None, SkipSearch: true}, Args{Query: "xin chào"})
	if err != nil {
		t.Fatalf("skip phải Ok, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("skip phải rỗng, got %d", len(got))
	}
}
