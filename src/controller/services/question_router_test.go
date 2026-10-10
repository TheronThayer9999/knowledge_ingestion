package services

import (
	"testing"

	"knowledge_ingestion/src/common/constants"
)

func TestRoute(t *testing.T) {
	cases := []struct {
		typ        QuestionType
		tool       string
		limit      int
		skipSearch bool
	}{
		{QuestionLookup, ToolKnowledgeSearch, constants.SEARCH_DEFAULT_LIMIT, false},
		{QuestionSummary, ToolKnowledgeSearch, 10, false},
		{QuestionCompare, ToolKnowledgeSearch, 10, false},
		{QuestionProcedure, ToolKnowledgeSearch, 8, false},
		{QuestionReasoning, ToolKnowledgeSearch, 10, false},
		{QuestionCalc, ToolKnowledgeSearch, constants.SEARCH_DEFAULT_LIMIT, false},
		{QuestionChitchat, ToolNone, 0, true},
		{QuestionScenario, ToolKnowledgeSearch, 8, false},
		{QuestionType(99), ToolKnowledgeSearch, constants.SEARCH_DEFAULT_LIMIT, false},
	}
	for _, tc := range cases {
		got := Route(tc.typ)
		if got.Tool != tc.tool || got.Limit != tc.limit || got.SkipSearch != tc.skipSearch {
			t.Errorf("Route(%v) = %+v, want tool=%q limit=%d skip=%v",
				tc.typ, got, tc.tool, tc.limit, tc.skipSearch)
		}
	}
}
