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

	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

// maxIterations chặn vòng agentic — agent gọi tool tối đa bấy nhiêu lượt rồi
// phải trả lời. Câu dễ tốn 1-2 lượt, câu khó (liệt kê nhiều Điều, so sánh 2
// file) tự gọi thêm, chặn 12 để không bùng chi phí nhưng đủ cho model yếu
// (OpenRouter free từng cháy 8 lượt rồi abort thành 500).
const maxIterations = 12

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
	// Summarization middleware: tự nén lịch sử hội thoại khi vượt ngưỡng
	// (thay cắt cứng maxHistoryTurns) — xem summarize.go.
	summ, err := newSummarizeMiddleware(model)
	if err != nil {
		return nil, fmt.Errorf("eino agent: dựng summarization middleware: %w", err)
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
		Handlers:      []adk.ChatModelAgentMiddleware{summ},
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

// maxHistoryTurns là lưới an toàn cuối (số lượt tối đa gửi vào agent) — phần
// nén chính do summarization middleware lo khi vượt ngưỡng (summarize.go),
// nên giờ giữ được 40 lượt thay vì cắt cứng 6 như trước.
const maxHistoryTurns = 40

// citationRe bắt nhãn trích dẫn [^n] của lượt trước. Số n của lượt cũ không
// khớp số của lượt đang chạy nên bỏ đi, tránh model nhầm nguồn.
var citationRe = regexp.MustCompile(`\[\^\d+\]`)

func stripCitations(s string) string {
	return strings.TrimSpace(citationRe.ReplaceAllString(s, ""))
}

// RunResult là output 1 lượt chat: câu trả lời markdown + hits đã gom qua các
// lượt gọi tool (để dựng cites, khớp [^n] trong answer) + cờ đã đọc tài liệu
// (để dán nhãn question_type, kể cả khi không gom được hit nào) + reasoning —
// phần model tự nghĩ trước khi đáp, chỉ có khi backend bật `think`.
type RunResult struct {
	Answer    string
	Reasoning string
	Hits      []*domain.ScoredChunk
	Searched  bool
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
	// Mỗi lượt trong loop có thể kèm reasoning riêng (nghĩ → gọi tool → nghĩ
	// tiếp) nên gom theo thứ tự, không lấy mỗi lượt cuối — người đọc muốn thấy
	// cả quá trình dẫn tới câu trả lời.
	var reasonings []string
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
			msg := out.MessageOutput.Message
			answer = messageText(msg)
			if r := strings.TrimSpace(msg.ReasoningContent); r != "" {
				reasonings = append(reasonings, r)
			}
		}
	}
	if strings.TrimSpace(answer) == "" {
		// Model free thỉnh thoảng trả content rỗng (hoặc dạng multi-part) —
		// log rõ để chẩn đoán thay vì câm lặng trả answer rỗng.
		logs.Warnw("eino agent: answer rỗng cuối lượt", "query", query)
	}
	var hits = make([]*domain.ScoredChunk, 0)
	searched := false
	if scope, ok := ScopeFrom(ctx); ok && scope.Collector != nil {
		hits, searched = scope.Collector.Snapshot()
	}
	return RunResult{
		Answer:    answer,
		Reasoning: strings.Join(reasonings, "\n\n"),
		Hits:      hits,
		Searched:  searched,
	}, nil
}
