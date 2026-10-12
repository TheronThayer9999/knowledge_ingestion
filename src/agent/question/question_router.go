package question

import (
	"knowledge_ingestion/src/agent/tools"
	"knowledge_ingestion/src/common/constants"
)

// Tên tool giữ alias để test/usecase cũ không vỡ — nguồn thật nằm ở tools.
const (
	ToolKnowledgeSearch = tools.KnowledgeSearch
	ToolNone            = tools.None
)

// RouteDecision alias về tools.Decision — router chỉ chọn tool + params,
// thực thi nằm ở tools.Registry.Execute.
type RouteDecision = tools.Decision

// Route trả default tool + params theo loại — dùng làm đường lui khi LLM lỗi
// và để kiểm chứng quyết định của LLM (tool lạ thì về default này). Đường
// chính là LLM vòng 1 (decide), không phải switch này.
func Route(t QuestionType) RouteDecision {
	switch t {
	case QuestionSummary, QuestionCompare, QuestionReasoning:
		return RouteDecision{Tool: ToolKnowledgeSearch, Limit: 10}
	case QuestionProcedure, QuestionScenario:
		return RouteDecision{Tool: ToolKnowledgeSearch, Limit: 8}
	case QuestionCalc:
		return RouteDecision{Tool: ToolKnowledgeSearch, Limit: constants.SEARCH_DEFAULT_LIMIT}
	case QuestionChitchat:
		return RouteDecision{Tool: ToolNone, SkipSearch: true}
	default:
		return RouteDecision{Tool: ToolKnowledgeSearch, Limit: constants.SEARCH_DEFAULT_LIMIT}
	}
}
