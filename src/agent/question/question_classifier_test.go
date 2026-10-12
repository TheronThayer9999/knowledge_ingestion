package question

import (
	"context"
	"errors"
	"strings"
	"testing"

	"knowledge_ingestion/src/domain"
)

// mustTestPrompts load prompt classify thật từ repo cho test — live test và
// guard test dùng chung, fail rõ khi file thiếu.
func mustTestPrompts(t *testing.T) Prompts {
	t.Helper()
	p, err := LoadPrompts("../../../prompts")
	if err != nil {
		t.Fatalf("LoadPrompts: %v", err)
	}
	return p
}

// fakeQuestionLLM giả domain.ILLM cho classifier — usecase có fakeLLM riêng,
// question tự stub để không phụ thuộc test package khác.
type fakeQuestionLLM struct {
	answer string
	err    error
}

func (f *fakeQuestionLLM) ListModels(_ context.Context) ([]string, error) {
	return []string{"test-model"}, nil
}

func (f *fakeQuestionLLM) Chat(_ context.Context, _ []domain.ChatMessage, _ domain.ChatOptions) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.answer, nil
}

func TestLLMClassify(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		answer string
		err    error
		model  string
		query  string
		want   QuestionType
	}{
		{"slug chuan", "compare", nil, "m", "so sánh A và B", QuestionCompare},
		// LLM trả thừa chữ ("Loại: ...") vẫn parse được.
		{"tra loi thua", "Loại: scenario nhé", nil, "m", "nếu tôi...", QuestionScenario},
		{"tra loi rac", "không hiểu bạn hỏi gì", nil, "m", "abc", QuestionLookup},
		{"llm loi", "", errors.New("timeout"), "m", "abc", QuestionLookup},
		{"thieu model", "compare", nil, "", "so sánh A và B", QuestionLookup},
		{"thieu query", "compare", nil, "m", "   ", QuestionLookup},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewLLMClassifier(&fakeQuestionLLM{answer: tc.answer, err: tc.err}, Prompts{ClassifySystem: "test-classify"})
			if got := c.Classify(ctx, tc.model, tc.query); got != tc.want {
				t.Fatalf("Classify = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLLMClassifyNilSafe(t *testing.T) {
	var c *LLMClassifier
	if got := c.Classify(context.Background(), "m", "q"); got != QuestionLookup {
		t.Fatalf("nil classifier = %v, want lookup", got)
	}
	if got := NewLLMClassifier(nil, Prompts{ClassifySystem: "test-classify"}).Classify(context.Background(), "m", "q"); got != QuestionLookup {
		t.Fatalf("nil llm = %v, want lookup", got)
	}
}

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want QuestionType
		ok   bool
	}{
		{"compare", QuestionCompare, true},
		{" Compare ", QuestionCompare, true},
		{"CHITCHAT", QuestionChitchat, true},
		{"lookup", QuestionLookup, true},
		{"", QuestionLookup, false},
		{"   ", QuestionLookup, false},
		{"loai: scenario nhe", QuestionLookup, false}, // Parse khop chinh xac, khong quet chuoi con
		{"khong-co-loai-nay", QuestionLookup, false},
	}
	for _, tc := range cases {
		if got, ok := Parse(tc.in); got != tc.want || ok != tc.ok {
			t.Errorf("Parse(%q) = (%v, %v), want (%v, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestLoadPromptsFromRepo(t *testing.T) {
	p, err := LoadPrompts("../../../prompts")
	if err != nil {
		t.Fatalf("LoadPrompts: %v", err)
	}
	if !strings.Contains(p.ClassifySystem, "slug") {
		t.Error("prompt classify thieu huong dan tra slug")
	}
}
