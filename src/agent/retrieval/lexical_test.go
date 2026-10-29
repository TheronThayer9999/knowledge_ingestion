package retrieval

import (
	"testing"

	"knowledge_ingestion/src/domain"
)

func chunk(pointID, text string) *domain.ScoredChunk {
	return &domain.ScoredChunk{PointID: pointID, ArticleID: 1, Text: text}
}

// Chunk phủ ĐỀU các từ của query phải đứng trên chunk chỉ khớp một từ — đó là
// toàn bộ lý do nửa keyword cần xếp hạng lại thay vì ăn thứ tự ngẫu nhiên của
// Scroll.
func TestRankLexicalPrefersWiderCoverage(t *testing.T) {
	hits := []*domain.ScoredChunk{
		chunk("a", "Bên có lỗi phải bồi thường thiệt hại thực tế phát sinh."),
		chunk("b", "Trách nhiệm dân sự là trách nhiệm bồi thường thiệt hại do vi phạm nghĩa vụ dân sự."),
	}
	got := RankLexical("trách nhiệm dân sự bồi thường thiệt hại", hits, 2)
	if len(got) != 2 {
		t.Fatalf("số hit = %d, want 2", len(got))
	}
	if got[0].PointID != "b" {
		t.Errorf("hạng nhất = %q, want %q (phủ đủ từ hơn)", got[0].PointID, "b")
	}
	if got[0].Score <= got[1].Score {
		t.Errorf("score %v <= %v, mảng xếp sai điểm", got[0].Score, got[1].Score)
	}
}

// Một từ lặp lại 20 lần không được thắng chunk phủ hai từ khác nhau — trần
// bão hòa lexicalSaturate chặn đúng kiểu phình điểm này.
func TestRankLexicalSaturatesRepetition(t *testing.T) {
	hits := []*domain.ScoredChunk{
		chunk("lap", "hợp đồng hợp đồng hợp đồng hợp đồng hợp đồng hợp đồng"),
		chunk("phu", "hợp đồng dân sự có hiệu lực khi"),
	}
	got := RankLexical("hợp đồng dân sự", hits, 2)
	if got[0].PointID != "phu" {
		t.Errorf("hạng nhất = %q, want %q (lặp 1 từ không thắng phủ 2 từ)", got[0].PointID, "phu")
	}
}

// Từ dài nặng ký hơn từ ngắn: "trách nhiệm" phân biệt chủ đề tốt hơn "có".
func TestRankLexicalWeightsLongTerms(t *testing.T) {
	hits := []*domain.ScoredChunk{
		chunk("ngan", "có có có"),
		chunk("dai", "trách nhiệm hình sự"),
	}
	got := RankLexical("trách nhiệm có", hits, 2)
	if got[0].PointID != "dai" {
		t.Errorf("hạng nhất = %q, want %q", got[0].PointID, "dai")
	}
}

func TestRankLexicalCutsAndKeepsInputClean(t *testing.T) {
	in := []*domain.ScoredChunk{chunk("a", "hợp đồng"), chunk("b", "hợp đồng thuê nhà"), chunk("c", "không liên quan")}
	got := RankLexical("hợp đồng thuê nhà", in, 2)
	if len(got) != 2 {
		t.Fatalf("cắt limit = %d hit, want 2", len(got))
	}
	if got[0].PointID != "b" {
		t.Errorf("hạng nhất = %q, want %q", got[0].PointID, "b")
	}
	// Input không được bị sửa: RankLexical copy chunk, Score gốc vẫn 0 — nếu
	// ghi đè thì caller (FuseRRF, log debug) thấy điểm changed khó tra.
	for _, h := range in {
		if h.Score != 0 {
			t.Errorf("input %q bị ghi Score = %v", h.PointID, h.Score)
		}
	}
}

// Query toàn dấu câu (không còn từ nào) thì thà trả nguyên mẫu còn hơn trả
// rỗng — search vẫn phải có kết quả.
func TestRankLexicalFallbackOnNoTerms(t *testing.T) {
	in := []*domain.ScoredChunk{chunk("a", "x"), chunk("b", "y"), chunk("c", "z")}
	got := RankLexical("... !!!", in, 2)
	if len(got) != 2 || got[0].PointID != "a" {
		t.Errorf("got = %d hit, đầu %q, want giữ nguyên thứ tự, cắt 2", len(got), got[0].PointID)
	}
}

// Điểm bằng nhau thì PointID quyết định — test phải chạy giống nhau trên mọi
// máy (map của Go không giữ thứ tự).
func TestRankLexicalIsDeterministic(t *testing.T) {
	hits := []*domain.ScoredChunk{chunk("zz", "hợp đồng"), chunk("aa", "hợp đồng"), chunk("mm", "hợp đồng")}
	for i := 0; i < 20; i++ {
		got := RankLexical("hợp đồng", hits, 3)
		if got[0].PointID != "aa" || got[2].PointID != "zz" {
			t.Fatalf("lần %d: %q..%q, muốn aa..zz", i, got[0].PointID, got[2].PointID)
		}
	}
}
