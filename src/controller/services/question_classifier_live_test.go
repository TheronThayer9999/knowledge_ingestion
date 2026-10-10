package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"knowledge_ingestion/src/config"
	llmollama "knowledge_ingestion/src/infrastructure/llm/ollama"
)

// loadEvalCases đọc bộ câu hỏi từ testdata/eval_questions.txt — sửa câu hỏi
// thì sửa file txt, không đụng code test.
func loadEvalCases(t *testing.T) []struct {
	query string
	want  QuestionType
} {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "eval_questions.txt"))
	if err != nil {
		t.Fatalf("đọc file câu hỏi: %v", err)
	}
	var cases []struct {
		query string
		want  QuestionType
	}
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			t.Fatalf("dòng %d sai định dạng (thiếu |): %q", i+1, line)
		}
		slug := strings.TrimSpace(parts[0])
		want := parseQuestionType(slug)
		if want.String() != slug {
			t.Fatalf("dòng %d slug lạ: %q", i+1, slug)
		}
		cases = append(cases, struct {
			query string
			want  QuestionType
		}{query: strings.TrimSpace(parts[1]), want: want})
	}
	if len(cases) == 0 {
		t.Fatal("file câu hỏi rỗng")
	}
	return cases
}

// TestClassifyLiveEval đánh giá classifier bằng LLM cloud thật trên bộ câu
// hỏi mẫu (2 câu/loại × 8 loại). SKIP khi thiếu key. Tốn ~16 lượt Chat ngắn.
// Đỏ ở câu nào thì log rõ để sửa prompt/instruction, không sửa đáp án mẫu.
func TestClassifyLiveEval(t *testing.T) {
	cfg, err := config.Load(filepath.Join("..", "..", "..", "configs", "config.json"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	lc := cfg.GetLLM()
	if lc.APIKey == "" || lc.Model == "" {
		t.Skip("thiếu llm key/model, skipping live eval")
	}

	llm, err := llmollama.New(lc)
	if err != nil {
		t.Fatalf("New llm: %v", err)
	}
	c := NewLLMClassifier(llm)
	cases := loadEvalCases(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	correct := 0
	for _, tc := range cases {
		got := c.Classify(ctx, lc.Model, tc.query)
		status := "OK"
		if got != tc.want {
			status = "SAI"
		} else {
			correct++
		}
		t.Logf("[%s] %-60q -> %-9v (muốn %v)", status, tc.query, got, tc.want)
		if got != tc.want {
			t.Errorf("Classify(%q) = %v, muốn %v", tc.query, got, tc.want)
		}
	}
	t.Logf("độ chính xác: %d/%d", correct, len(cases))
}
