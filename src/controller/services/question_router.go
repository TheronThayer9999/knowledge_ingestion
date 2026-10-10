package services

import (
	"knowledge_ingestion/src/common/constants"
)

// Tool agent được gọi ở vòng 2 — router vòng 1 chỉ chọn trong 2 giá trị này.
const (
	// ToolKnowledgeSearch tìm đoạn trích trong tài liệu upload (bọc
	// HybridSearcher.SearchHybrid khi port sang Eino InvokableTool).
	ToolKnowledgeSearch = "knowledge_search"
	// ToolNone không gọi tool nào (chitchat) — đáp thẳng, đỡ tốn embed+Qdrant.
	ToolNone = "none"
)

// RouteDecision là output vòng 1b: tool nào + tham số nào cho vòng 2. DTO của
// caller (limit/threshold tự chỉ định) thắng router — router chỉ là default
// theo loại khi caller không nói gì.
type RouteDecision struct {
	Type QuestionType
	Tool string
	// Limit số hit vòng 2 lấy; SkipSearch true thì khỏi search luôn.
	Limit      int
	Threshold  float32
	SkipSearch bool
}

// Route map loại câu hỏi → tool + params (§8 vòng 1b). Pure function, không
// dính LLM/infra nên test thẳng bảng 8 loại.
func Route(t QuestionType) RouteDecision {
	switch t {
	case QuestionSummary, QuestionCompare, QuestionReasoning:
		return RouteDecision{Type: t, Tool: ToolKnowledgeSearch, Limit: 10}
	case QuestionProcedure, QuestionScenario:
		return RouteDecision{Type: t, Tool: ToolKnowledgeSearch, Limit: 8}
	case QuestionCalc:
		return RouteDecision{Type: t, Tool: ToolKnowledgeSearch, Limit: constants.SEARCH_DEFAULT_LIMIT}
	case QuestionChitchat:
		return RouteDecision{Type: t, Tool: ToolNone, SkipSearch: true}
	default:
		return RouteDecision{Type: QuestionLookup, Tool: ToolKnowledgeSearch, Limit: constants.SEARCH_DEFAULT_LIMIT}
	}
}
