package retrieval

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"knowledge_ingestion/src/domain"
)

// lexicalSaturate trần số lần một từ được đếm khi so hai chunk cùng độ phủ:
// chunk lặp một từ 20 lần không đáng xếp trên chunk nhắc nó 3 lần — cái gì vượt
// mức này coi như "đã nói đủ", để một từ láy đi láy lại không trôi lên đầu bảng.
const lexicalSaturate = 3

// RankLexical xếp nửa keyword theo độ phủ từ rồi cắt limit — điểm [0..1] ghi
// vào Score để debug, thứ tự mới là thứ RRF dùng.
//
// Nó tồn tại vì Qdrant chỉ có full-text MATCH (filter "có ít nhất một từ của
// query"), không có scoring kiểu BM25 cho payload text: Scroll trả đúng
// `limit` point theo thứ tự nội bộ của segment, tức là một mẫu NGẪU NHIÊN
// trong tập khớp. Không xếp hạng lại thì FuseRRF nhận rank rác ở nửa text,
// hợp nhất xong ranking bị kéo xuống thay vì được nâng lên — đó là lý do
// baseline keyword hit@1 thấp.
//
// Điểm = Σ trọng số những từ query xuất hiện trong chunk / Σ trọng số toàn bộ
// từ của query, nên 1.0 nghĩa là "nói đủ mọi từ". Trọng số lấy theo ĐỘ DÀI từ
// (proxy miễn phí cho IDF: trong tiếng Việt, từ càng dài càng hiếm và càng chỉ
// đúng chủ đề — "trách nhiệm" phân biệt tốt hơn "có"), vì vậy query 5 từ dài
// ăn điểm gần tuyệt đối khi phủ đủ còn chunk chỉ khớp một từ ngắn bị đẩy xuống
// đáy. Số lần nhắc chỉ phân hạng khi hai chunk ngang độ phủ (lexicalSaturate).
//
// Pure function, không đụng Qdrant: test cho slice vào và so thứ tự ra.
func RankLexical(query string, hits []*domain.ScoredChunk, limit int) []*domain.ScoredChunk {
	weights, total := termWeights(tokenize(query))
	if total == 0 {
		// Query không còn từ nào sau khi tách (toàn dấu câu) — thà giữ nguyên
		// thứ tự Qdrant trả còn hơn trả rỗng.
		return cutLexical(hits, limit)
	}
	ranked := make([]lexicalHit, 0, len(hits))
	for _, h := range hits {
		if h == nil {
			continue
		}
		ranked = append(ranked, scoreChunk(h, weights, total))
	}
	sort.SliceStable(ranked, func(i, j int) bool { return betterLexical(ranked[i], ranked[j]) })
	out := make([]*domain.ScoredChunk, 0, len(ranked))
	for _, r := range ranked {
		chunk := *r.chunk
		chunk.Score = float32(r.score)
		out = append(out, &chunk)
	}
	// Cắt ở cuối bằng đúng helper nhánh query-rỗng dùng, để limit<=0 có cùng
	// nghĩa ở cả hai đường: "giữ hết", không phải "trả rỗng".
	return cutLexical(out, limit)
}

// lexicalHit là 1 chunk kèm ba con số xếp hạng: điểm phủ từ, số từ KHÁC NHAU
// nó phủ, và tổng số lần xuất hiện (bão hòa) — hai cái sau chỉ dùng làm
// tie-break.
type lexicalHit struct {
	chunk    *domain.ScoredChunk
	score    float64
	covered  int
	mentions int
}

// scoreChunk cho điểm theo SỰ CÓ MẶT của từng từ, không theo số lần: chunk
// tiếng Việt dài vài trăm ký tự, "nhắc 6 lần" thường là lặp lại một mệnh đề
// chứ không phải liên quan gấp 6 lần. Vì vậy phủ đủ query luôn thắng phủ một
// nửa, còn lặp bao nhiêu chỉ phân hạng khi hai chunk ngang nhau về độ phủ.
func scoreChunk(h *domain.ScoredChunk, weights map[string]float64, total float64) lexicalHit {
	counts := tokenCounts(h.Text)
	var got float64
	var covered, mentions int
	for term, weight := range weights {
		n := counts[term]
		if n == 0 {
			continue
		}
		covered++
		got += weight
		mentions += min(n, lexicalSaturate)
	}
	return lexicalHit{chunk: h, score: got / total, covered: covered, mentions: mentions}
}

// betterLexical so hai chunk theo thứ tự ưu tiên: độ phủ có trọng số → số từ
// khác nhau → số lần nhắc (bão hòa) → PointID (thứ tự ổn định để test không
// lật kèo, vì map của Go không giữ thứ tự).
func betterLexical(a, b lexicalHit) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	if a.covered != b.covered {
		return a.covered > b.covered
	}
	if a.mentions != b.mentions {
		return a.mentions > b.mentions
	}
	return a.chunk.PointID < b.chunk.PointID
}

func cutLexical(hits []*domain.ScoredChunk, limit int) []*domain.ScoredChunk {
	if limit <= 0 || len(hits) <= limit {
		return hits
	}
	return hits[:limit]
}

// tokenize tách query thành từ thường (bỏ dấu câu, gộp khoảng trắng). Tiếng
// Việt viết cách nhau theo âm tiết nên tách theo khoảng trắng + bỏ ký tự
// không phải chữ/số là đủ bám từ — không cần bộ cắt câu tiếng Việt (cái đó
// mới là thứ phải kéo thư viện về, và nó chỉ cần cho truy vấn, không cho ranking).
func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// termWeights quy đổi danh sách từ của query thành trọng số theo độ dài rune
// (từ lặp trong query chỉ tính một lần) kèm tổng trọng số.
func termWeights(terms []string) (map[string]float64, float64) {
	weights := make(map[string]float64, len(terms))
	var total float64
	for _, t := range terms {
		w := float64(utf8.RuneCountInString(t))
		if w < 2 {
			continue
		}
		if _, ok := weights[t]; ok {
			continue
		}
		weights[t] = w
		total += w
	}
	return weights, total
}

// tokenCounts đếm số lần từng từ xuất hiện trong text của chunk.
func tokenCounts(text string) map[string]int {
	toks := tokenize(text)
	counts := make(map[string]int, len(toks))
	for _, t := range toks {
		counts[t]++
	}
	return counts
}
