package usecase

import (
	"context"

	einomodel "github.com/cloudwego/eino/components/model"

	"knowledge_ingestion/src/agent/question"
)

// IClassifier là vòng 1 dán nhãn câu hỏi (lookup / spreadsheet / hợp đồng...).
// Chat và search chỉ cần biết "nhãn là gì" — model nào, prompt nào, retry ra sao
// là việc của package question. Nhờ interface này, test dựng được fake trả sẵn
// nhãn mà không phải kéo theo client LLM (loader mới là chỗ duy nhất biết
// *question.LLMClassifier).
//
// client là client của provider CỦA LƯỢT ĐÓ: lượt chat chạy trên kết nối riêng
// của user thì bước phân loại cũng chạy trên key của user đó — không có call
// nào lấy key của hệ thống. Rỗng thì dùng client mặc định (provider chung).
type IClassifier interface {
	Classify(ctx context.Context, client einomodel.BaseChatModel, model, query string) question.QuestionType
}
