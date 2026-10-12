package eino

import (
	"context"
	"errors"
	"strings"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

// scriptModel giả ChatModel theo kịch bản nhiều lượt — lượt i trả turns[i],
// hết kịch bản thì lặp câu cuối. Đủ để test vòng ReAct không cần mạng/LLM.
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
	return nil, errors.New("eino test: stream không dùng trong test")
}

func toolCallTurn(args string) *schema.Message {
	return schema.AssistantMessage("", []schema.ToolCall{{
		ID:   "c1",
		Type: "function",
		Function: schema.FunctionCall{
			Name:      KnowledgeSearchName,
			Arguments: args,
		},
	}})
}

func testAgent(t *testing.T, model einomodel.BaseChatModel) (*ChatAgent, context.Context, *HitCollector) {
	t.Helper()
	reg, _ := testRegistry([]float32{0.1},
		[]*domain.ScoredChunk{{PointID: "p1", Score: 0.9, ArticleID: 8, ChunkIndex: 0, PageNum: 0, Text: "đoạn A"}},
		[]int64{8})
	tl, err := NewKnowledgeSearchTool(reg)
	if err != nil {
		t.Fatalf("tool: %v", err)
	}
	agent, err := NewChatAgent(model, "test-instruction", tl)
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	ctx, col := WithCollector(WithRequest(context.Background(), Request{UserID: 7}))
	return agent, ctx, col
}

// Vòng ReAct đủ: model gọi tool → tool trả hits → model tổng hợp giữ [^1].
func TestAgentToolLoop(t *testing.T) {
	model := &scriptModel{turns: []*schema.Message{
		toolCallTurn(`{"query":"so sánh A và B","limit":10}`),
		{Role: schema.Assistant, Content: "Kết quả so sánh[^1]."},
	}}
	agent, ctx, col := testAgent(t, model)

	res, err := agent.Run(ctx, nil, "so sánh A và B")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Answer, "[^1]") {
		t.Fatalf("answer phải giữ [^1], got %q", res.Answer)
	}
	if len(col.Hits) != 1 || col.Hits[0].ArticleID != 8 {
		t.Fatalf("collector phải có hit cho cites, got %+v", col.Hits)
	}
	if len(res.Hits) != 1 {
		t.Fatalf("result hits = %d, want 1", len(res.Hits))
	}
}

// Không cần tool thì đáp thẳng, hits rỗng (chitchat đỡ tốn embed+Qdrant).
func TestAgentNoTool(t *testing.T) {
	model := &scriptModel{turns: []*schema.Message{
		{Role: schema.Assistant, Content: "Chào bạn!"},
	}}
	agent, ctx, _ := testAgent(t, model)

	res, err := agent.Run(ctx, nil, "xin chào")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Answer != "Chào bạn!" || len(res.Hits) != 0 {
		t.Fatalf("got %+v", res)
	}
}

// History đi vào agent để câu nối tiếp hiểu ngữ cảnh.
func TestAgentHistory(t *testing.T) {
	var gotInput []*schema.Message
	model := &captureModel{answer: "Rõ rồi.", gotInput: &gotInput}
	agent, ctx, _ := testAgent(t, model)

	_, err := agent.Run(ctx, []dtos.ChatHistoryItem{{Role: "user", Content: "Nghị định 123 là gì?"}}, "còn hiệu lực không?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(gotInput) != 3 || gotInput[0].Role != schema.System || gotInput[1].Content != "Nghị định 123 là gì?" || gotInput[2].Content != "còn hiệu lực không?" {
		t.Fatalf("phải là [system instruction, history, câu hiện tại], got %+v", gotInput)
	}
}

type captureModel struct {
	answer   string
	gotInput *[]*schema.Message
}

func (m *captureModel) Generate(_ context.Context, input []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	*m.gotInput = input
	return &schema.Message{Role: schema.Assistant, Content: m.answer}, nil
}

func (m *captureModel) Stream(_ context.Context, _ []*schema.Message, _ ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("eino test: stream không dùng trong test")
}

func TestNewChatAgentGuards(t *testing.T) {
	reg, _ := testRegistry([]float32{0.1}, nil, nil)
	tl, err := NewKnowledgeSearchTool(reg)
	if err != nil {
		t.Fatalf("tool: %v", err)
	}
	if _, err := NewChatAgent(nil, "x", tl); err == nil {
		t.Fatal("model nil phải lỗi")
	}
	if _, err := NewChatAgent(&scriptModel{turns: []*schema.Message{{Role: schema.Assistant}}}, "x"); err == nil {
		t.Fatal("không tool phải lỗi")
	}
}
