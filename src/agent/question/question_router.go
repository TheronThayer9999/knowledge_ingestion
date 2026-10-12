package question

import (
	"knowledge_ingestion/src/common/constants"
)

// RouteResult là default tham số search theo loại câu hỏi — đường /search
// deterministic (classify → route → hybrid) chỉ cần 2 thứ này: lấy bao nhiêu
// hit, và có bỏ qua search không (chitchat). Tool nào thì không cần chọn vì
// search luôn là 1 pipeline hybrid duy nhất.
type RouteResult struct {
	// Limit số hit lấy; SkipSearch true thì khỏi search luôn.
	Limit      int
	SkipSearch bool
}

// Route trả default params theo loại — dùng làm đường lui khi LLM lỗi và để
// caller ép limit thắng khi cần. Không model thì lookup, không tốn lượt LLM.
func Route(t QuestionType) RouteResult {
	switch t {
	case QuestionSummary, QuestionCompare, QuestionReasoning:
		return RouteResult{Limit: 10}
	case QuestionProcedure, QuestionScenario:
		return RouteResult{Limit: 8}
	case QuestionCalc:
		return RouteResult{Limit: constants.SEARCH_DEFAULT_LIMIT}
	case QuestionChitchat:
		return RouteResult{SkipSearch: true}
	default:
		return RouteResult{Limit: constants.SEARCH_DEFAULT_LIMIT}
	}
}
