package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	einopilot "knowledge_ingestion/src/agent/eino"
	"knowledge_ingestion/src/agent/port"
	"knowledge_ingestion/src/agent/question"
	"knowledge_ingestion/src/agent/retrieval"
	"knowledge_ingestion/src/agent/tools"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// fakeLLM giả domain.ILLM cho tool classify_question trong loop agent.
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

// scriptModel giả ChatModel Eino theo kịch bản nhiều lượt — lượt i trả
// turns[i], hết kịch bản lặp câu cuối. Đủ test loop agent không cần mạng.
type scriptModel struct {
	turns []*schema.Message
	i     int
}

func (m *scriptModel) Generate(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	t := m.turns[m.i]
	if m.i < len(m.turns)-1 {
		m.i++
	}
	return t, nil
}

func (m *scriptModel) Stream(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("test: stream không dùng")
}

func toolCallTurn(name, args string) *schema.Message {
	return schema.AssistantMessage("", []schema.ToolCall{{
		ID:   "c1",
		Type: "function",
		Function: schema.FunctionCall{
			Name:      name,
			Arguments: args,
		},
	}})
}

// newChatSvc dựng chat agentic thật (agent 2 tool + registry thật, chỉ fake
// hạ tầng và model theo kịch bản). classifyAnswer là nhãn tool classify trả.
func newChatSvc(classifyAnswer string, turns []*schema.Message, vec *fakeSearchVectors) (port.IChatter, *fakeSearchVectors) {
	if vec == nil {
		vec = &fakeSearchVectors{}
	}
	reg := tools.NewRegistry(retrieval.NewHybridSearcher(
		&fakeSearchEmbedder{vec: []float32{0.1}},
		vec,
		&fakeSearchArticles{doneIDs: []int64{8}},
	))
	classifier, err := einopilot.NewClassifyTool(
		question.NewLLMClassifier(&fakeLLM{answer: classifyAnswer}, question.Prompts{ClassifySystem: "test-classify"}))
	if err != nil {
		panic(err)
	}
	searchTool, err := einopilot.NewKnowledgeSearchTool(reg)
	if err != nil {
		panic(err)
	}
	agent, err := einopilot.NewChatAgent(&scriptModel{turns: turns}, "test-instruction", classifier, searchTool)
	if err != nil {
		panic(err)
	}
	return NewChatService(agent, &fakeSearchUser{id: 7}), vec
}

func classifyTurn(q string) *schema.Message {
	return toolCallTurn(einopilot.ClassifyName, `{"question":`+q+`}`)
}

func TestChat_FullPipeline(t *testing.T) {
	vec := &fakeSearchVectors{hits: []*domain.ScoredChunk{
		{PointID: "p1", Score: 0.9, ArticleID: 8, ChunkIndex: 2, PageNum: 4, Text: "Điều 13 phạt 0.05%/ngày"},
	}}
	svc, _ := newChatSvc("lookup", []*schema.Message{
		classifyTurn(`"Mức phạt?"`),
		toolCallTurn(einopilot.KnowledgeSearchName, `{"query":"Mức phạt?","limit":5}`),
		{Role: schema.Assistant, Content: "Mức phạt là **0.05%/ngày**[^1]."},
	}, vec)

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
	// Agent tự quyết đáp thẳng không gọi tool nào — embed/search không chạm tới.
	svc, _ := newChatSvc("chitchat", []*schema.Message{
		{Role: schema.Assistant, Content: "Chào bạn!"},
	}, &fakeSearchVectors{})

	res := svc.Chat(context.Background(), &dtos.ChatRequest{Message: "xin chào", Model: "m"})
	if res.Err != nil {
		t.Fatalf("unexpected err: %v", res.Err)
	}
	if len(res.Data.Cites) != 0 || res.Data.QuestionType != "lookup" {
		t.Fatalf("chitchat bỏ classify thì nhãn về lookup + rỗng cites, got %+v", res.Data)
	}
	if res.Data.Answer != "Chào bạn!" {
		t.Fatalf("answer = %q", res.Data.Answer)
	}
}

func TestChat_RequiresModel(t *testing.T) {
	svc, _ := newChatSvc("lookup", []*schema.Message{
		{Role: schema.Assistant, Content: "x"},
	}, nil)
	if res := svc.Chat(context.Background(), &dtos.ChatRequest{Message: "x"}); res.Err == nil {
		t.Fatal("thiếu model phải 400")
	}
}

func TestChat_AgentErrorPropagates(t *testing.T) {
	// Model gọi tool không tồn tại → runner lỗi → Chat trả lỗi ra ngoài.
	badTurn := toolCallTurn("khong_co_tool", `{}`)
	svc, _ := newChatSvc("lookup", []*schema.Message{badTurn}, nil)
	if res := svc.Chat(context.Background(), &dtos.ChatRequest{Message: "x", Model: "m"}); res.Err == nil {
		t.Fatal("lỗi agent phải lan ra")
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
