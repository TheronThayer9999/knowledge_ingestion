package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"knowledge_ingestion/src/controller/dtos"
)

// fakeChatSearch giả ISearchService — trả hits + question_type cố định.
type fakeChatSearch struct {
	hits  []*dtos.SearchHitResponse
	qtype string
	err   error
}

func (f *fakeChatSearch) Search(_ context.Context, _ *dtos.SearchRequest) dtos.Result[*dtos.SearchResponse] {
	if f.err != nil {
		return dtos.Fail[*dtos.SearchResponse](f.err)
	}
	return dtos.Ok(&dtos.SearchResponse{Hits: f.hits, Total: len(f.hits), QuestionType: f.qtype})
}

func TestChat_FullPipeline(t *testing.T) {
	search := &fakeChatSearch{
		hits: []*dtos.SearchHitResponse{
			{ArticleID: 8, ChunkIndex: 2, PageNum: 4, Score: 0.9, Text: "Điều 13 phạt 0.05%/ngày"},
		},
		qtype: "lookup",
	}
	svc := NewChatService(search, &fakeLLM{answer: "Mức phạt là **0.05%/ngày**[^1]."})

	res := svc.Chat(context.Background(), &dtos.ChatRequest{Message: "Mức phạt?", Model: "m"})
	if res.Err != nil {
		t.Fatalf("unexpected err: %v", res.Err)
	}
	if !strings.Contains(res.Data.Answer, "[^1]") {
		t.Fatalf("answer phải giữ chú thích [^1], got %q", res.Data.Answer)
	}
	if res.Data.QuestionType != "lookup" {
		t.Fatalf("question_type = %q", res.Data.QuestionType)
	}
	if len(res.Data.Cites) != 1 || res.Data.Cites[0].N != 1 || res.Data.Cites[0].ArticleID != 8 {
		t.Fatalf("cites sai: %+v", res.Data.Cites)
	}
	if res.Data.Cites[0].Snippet == "" {
		t.Fatal("cite phải có snippet preview")
	}
}

func TestChat_ChitchatNoHits(t *testing.T) {
	search := &fakeChatSearch{hits: []*dtos.SearchHitResponse{}, qtype: "chitchat"}
	svc := NewChatService(search, &fakeLLM{answer: "Chào bạn!"})

	res := svc.Chat(context.Background(), &dtos.ChatRequest{Message: "xin chào", Model: "m"})
	if res.Err != nil {
		t.Fatalf("unexpected err: %v", res.Err)
	}
	if len(res.Data.Cites) != 0 || res.Data.QuestionType != "chitchat" {
		t.Fatalf("chitchat phải rỗng cites, got %+v", res.Data)
	}
}

func TestChat_RequiresModel(t *testing.T) {
	svc := NewChatService(&fakeChatSearch{}, &fakeLLM{answer: "x"})
	if res := svc.Chat(context.Background(), &dtos.ChatRequest{Message: "x"}); res.Err == nil {
		t.Fatal("thiếu model phải 400")
	}
}

func TestChat_SearchErrorPropagates(t *testing.T) {
	svc := NewChatService(&fakeChatSearch{err: errors.New("qdrant down")}, &fakeLLM{answer: "x"})
	if res := svc.Chat(context.Background(), &dtos.ChatRequest{Message: "x", Model: "m"}); res.Err == nil {
		t.Fatal("lỗi search phải lan ra")
	}
}

func TestSnippet_RuneSafe(t *testing.T) {
	long := strings.Repeat("ệ", 300)
	got := snippet(long)
	if len([]rune(got)) != snippetLen+1 { // +1 ký tự …
		t.Fatalf("snippet phải cắt theo rune, got %d runes", len([]rune(got)))
	}
	if snippet("ngắn") != "ngắn" {
		t.Fatal("text ngắn phải giữ nguyên")
	}
}
