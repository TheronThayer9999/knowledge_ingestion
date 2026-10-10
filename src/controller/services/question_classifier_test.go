package services

import (
	"context"
	"errors"
	"testing"

	"knowledge_ingestion/src/domain"
)

// fakeLLM giả domain.ILLM: trả slug theo query, hoặc lỗi khi muốn.
type fakeLLM struct {
	answer string
	err    error
}

func (f *fakeLLM) ListModels(_ context.Context) ([]string, error) {
	return []string{"test-model"}, nil
}

func (f *fakeLLM) Chat(_ context.Context, _ []domain.ChatMessage, opts domain.ChatOptions) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if opts.Model == "" {
		return "", errors.New("model is required")
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
			c := NewLLMClassifier(&fakeLLM{answer: tc.answer, err: tc.err})
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
	if got := NewLLMClassifier(nil).Classify(context.Background(), "m", "q"); got != QuestionLookup {
		t.Fatalf("nil llm = %v, want lookup", got)
	}
}
