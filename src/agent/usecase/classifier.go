package usecase

import (
	"context"

	"knowledge_ingestion/src/agent/question"
)

// IClassifier là vòng 1 dán nhãn câu hỏi (lookup / spreadsheet / hợp đồng...).
// Chat và search chỉ cần biết "nhãn là gì" — model nào, prompt nào, retry ra sao
// là việc của package question. Nhờ interface này, test dựng được fake trả sẵn
// nhãn mà không phải kéo theo client LLM (loader mới là chỗ duy nhất biết
// *question.LLMClassifier).
type IClassifier interface {
	Classify(ctx context.Context, model, query string) question.QuestionType
}
