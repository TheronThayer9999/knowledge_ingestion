package services

import (
	"testing"

	"knowledge_ingestion/src/domain"
)

// TestFileTag khoá đúng thứ tự ưu tiên của field log `file`: storage key (blob
// thật trên kho) trước, rồi URL cho bài nguồn link ngoài, cuối cùng là "-" khi
// bài không có nguồn nào — worker luôn phải in ra một định danh, không để trống.
func TestFileTag(t *testing.T) {
	cases := []struct {
		name    string
		article *domain.Article
		want    string
	}{
		{
			name:    "bài upload dùng storage key",
			article: &domain.Article{StorageKey: "uploads/01a1768a-abc.txt", Name: "API Test A"},
			want:    "uploads/01a1768a-abc.txt",
		},
		{
			name:    "bài link ngoài dùng URL",
			article: &domain.Article{URL: "https://example.com/tai-lieu.pdf"},
			want:    "https://example.com/tai-lieu.pdf",
		},
		{
			name:    "có cả hai thì storage key thắng",
			article: &domain.Article{StorageKey: "uploads/k.pdf", URL: "https://example.com/x.pdf"},
			want:    "uploads/k.pdf",
		},
		{
			name:    "không có nguồn nào",
			article: &domain.Article{Name: "bài mồ côi"},
			want:    "-",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := fileTag(c.article); got != c.want {
				t.Errorf("fileTag = %q, muốn %q", got, c.want)
			}
		})
	}
}

// TestArticleTags — dòng log theo lô phải đọc ra được "id:tên (file)" để không
// phải tra DB mới biết kỳ quét đang chạm file nào.
func TestArticleTags(t *testing.T) {
	articles := []*domain.Article{
		{BaseModel: domain.BaseModel{ID: 63}, Name: "API Test A", StorageKey: "uploads/a.txt"},
		{BaseModel: domain.BaseModel{ID: 64}, Name: "Link ngoài", URL: "https://example.com/b.pdf"},
	}
	got := articleTags(articles)
	want := []string{
		"63:API Test A (uploads/a.txt)",
		"64:Link ngoài (https://example.com/b.pdf)",
	}
	if len(got) != len(want) {
		t.Fatalf("articleTags trả %d phần tử, muốn %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("articleTags[%d] = %q, muốn %q", i, got[i], want[i])
		}
	}
}
