package eino

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

// maxIterations chặn vòng agentic — agent gọi tool tối đa bấy nhiêu lượt rồi
// phải trả lời. Câu dễ tốn 1-2 lượt, câu khó tự gọi thêm, chặn 8 để không
// bùng chi phí.
const maxIterations = 8

// ChatAgent là agent chat: LLM tự quyết gọi tool nào theo vòng [nhận định →
// kế hoạch → gọi tool → đọc kết quả → quan sát → điều chỉnh] cho đến khi đủ
// căn cứ mới trả lời. Dựng một lần lúc startup (loader), chạy mỗi request
// qua Run. Thêm tool mới thì truyền thêm vào NewChatAgent, LLM tự thấy qua
// ToolInfo — không sửa prompt file.
type ChatAgent struct {
	agent  *adk.ChatModelAgent
	runner *adk.Runner
}

func NewChatAgent(model einomodel.ToolCallingChatModel, instruction string, tls ...einotool.BaseTool) (*ChatAgent, error) {
	if model == nil {
		return nil, fmt.Errorf("eino agent: model is required")
	}
	if len(tls) == 0 {
		return nil, fmt.Errorf("eino agent: cần ít nhất 1 tool")
	}
	base := make([]einotool.BaseTool, 0, len(tls))
	for _, t := range tls {
		if t == nil {
			return nil, fmt.Errorf("eino agent: tool nil")
		}
		base = append(base, t)
	}
	agent, err := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{
		Name:        "knowledge_chat",
		Description: "Trợ lý tri thức: dùng tools đọc tài liệu đã upload rồi trả lời kèm trích dẫn [^n]",
		Instruction: instruction,
		Model:       model,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: base,
			},
		},
		MaxIterations: maxIterations,
	})
	if err != nil {
		return nil, fmt.Errorf("eino agent: dựng ChatModelAgent: %w", err)
	}
	return &ChatAgent{
		agent:  agent,
		runner: adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: agent}),
	}, nil
}

// maxHistoryTurns giữ số lượt hội thoại gần nhất đưa vào agent — lượt cũ hơn
// không còn ảnh hưởng câu hỏi hiện tại, còn giữ thì tốn token.
const maxHistoryTurns = 6

// citationRe bắt nhãn trích dẫn [^n] của lượt trước. Số n của lượt cũ không
// khớp số của lượt đang chạy nên bỏ đi, tránh model nhầm nguồn.
var citationRe = regexp.MustCompile(`\[\^\d+\]`)

func stripCitations(s string) string {
	return strings.TrimSpace(citationRe.ReplaceAllString(s, ""))
}

// RunResult là output 1 lượt chat: câu trả lời markdown + hits đã gom qua các
// lượt gọi tool (để dựng cites, khớp [^n] trong answer) + cờ đã đọc tài liệu
// (để dán nhãn question_type, kể cả khi không gom được hit nào).
type RunResult struct {
	Answer   string
	Hits     []*domain.ScoredChunk
	Searched bool
}

// Run chạy agent 1 lượt: history + câu hỏi hiện tại đi vào, loop tool lặp
// cho đến khi model tự trả lời. Model theo từng request — truyền bằng
// adk.WithChatModelOptions của framework (agent dựng 1 lần lúc startup nên
// không kẹt model trong constructor). Lỗi model/tool/lặp quá trần đều trả về
// để caller map 500, không đoán thay.
func (a *ChatAgent) Run(ctx context.Context, model string, history []dtos.ChatHistoryItem, query string) (RunResult, error) {
	if a == nil || a.runner == nil {
		return RunResult{}, fmt.Errorf("eino agent: chưa được dựng")
	}
	msgs := make([]*schema.Message, 0, maxHistoryTurns+1)
	if len(history) > maxHistoryTurns {
		history = history[len(history)-maxHistoryTurns:]
	}
	for _, h := range history {
		if h.Role == "assistant" {
			msgs = append(msgs, schema.AssistantMessage(stripCitations(h.Content), nil))
		} else {
			msgs = append(msgs, schema.UserMessage(h.Content))
		}
	}
	msgs = append(msgs, schema.UserMessage(query))

	iter := a.runner.Run(ctx, msgs, adk.WithChatModelOptions([]einomodel.Option{einomodel.WithModel(model)}))
	var answer string
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev == nil {
			continue
		}
		if ev.Err != nil {
			return RunResult{}, fmt.Errorf("eino agent: %w", ev.Err)
		}
		if out := ev.Output; out != nil && out.MessageOutput != nil &&
			!out.MessageOutput.IsStreaming && out.MessageOutput.Message != nil {
			answer = out.MessageOutput.Message.Content
		}
	}
	var hits = make([]*domain.ScoredChunk, 0)
	searched := false
	if scope, ok := ScopeFrom(ctx); ok && scope.Collector != nil {
		hits = scope.Collector.Hits
		searched = scope.Collector.Searched
	}
	return RunResult{Answer: answer, Hits: hits, Searched: searched}, nil
}
