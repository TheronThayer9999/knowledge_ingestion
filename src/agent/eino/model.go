package eino

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	ollamaapi "github.com/ollama/ollama/api"

	"knowledge_ingestion/src/config"
)

// compile-time check: adapter phải thỏa contract model Eino để cắm vào
// adk.ChatModelAgent (Generate + Stream).
var _ model.BaseChatModel = (*OllamaChatModel)(nil)

// OllamaChatModel bọc Ollama /api/chat (thư viện chính thức) thành Eino
// ChatModel — khác client domain.ILLM ở chỗ Generate hiểu model.WithTools:
// gửi tools cho Ollama và parse tool_calls trả về, để agent gọi tool nhiều
// vòng. Model lấy theo thứ tự: ctx (WithModel — ChatRequest.Model theo
// request) → opts.WithModel → lỗi (không tự đoán, cùng hợp đồng với ILLM).
type OllamaChatModel struct {
	client      *ollamaapi.Client
	temperature float64
	maxTokens   int
}

func NewOllamaChatModel(cfg config.LLMConfig) (*OllamaChatModel, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("eino ollama: base_url is required")
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	for _, suffix := range []string{"/api/chat", "/api/tags", "/api"} {
		base = strings.TrimSuffix(base, suffix)
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("eino ollama: base_url sai: %w", err)
	}
	timeout := cfg.TimeoutSec
	if timeout <= 0 {
		timeout = 120
	}
	return &OllamaChatModel{
		// Lib chính thức không nhận Bearer key (chỉ ký local) nên key Cloud
		// đi theo Transport — local không key thì transport nguyên bản.
		client:      ollamaapi.NewClient(u, &http.Client{Timeout: time.Duration(timeout) * time.Second, Transport: authTransport(cfg.APIKey)}),
		temperature: cfg.Temperature,
		maxTokens:   cfg.MaxTokens,
	}, nil
}

// authTransport gắn Bearer token cho Ollama Cloud — không key thì thôi.
func authTransport(apiKey string) http.RoundTripper {
	if strings.TrimSpace(apiKey) == "" {
		return http.DefaultTransport
	}
	return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.Header.Set("Authorization", "Bearer "+apiKey)
		return http.DefaultTransport.RoundTrip(req)
	})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Generate gọi /api/chat một lượt (agent tự lặp nhiều lượt qua runner).
func (m *OllamaChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	co := model.GetCommonOptions(&model.Options{}, opts...)
	name := ModelFrom(ctx)
	if name == "" && co.Model != nil {
		name = strings.TrimSpace(*co.Model)
	}
	if name == "" {
		return nil, fmt.Errorf("eino ollama: model is required (lấy danh sách qua ListModels rồi truyền theo request)")
	}
	if len(input) == 0 {
		return nil, fmt.Errorf("eino ollama: messages is required")
	}
	temperature := m.temperature
	if co.Temperature != nil && *co.Temperature > 0 {
		temperature = float64(*co.Temperature)
	}
	maxTokens := m.maxTokens
	if co.MaxTokens != nil && *co.MaxTokens > 0 {
		maxTokens = *co.MaxTokens
	}

	stream := false
	var answer ollamaapi.ChatResponse
	err := m.client.Chat(ctx, &ollamaapi.ChatRequest{
		Model:    name,
		Messages: mapMessages(input),
		Stream:   &stream,
		Tools:    mapTools(co.Tools),
		Options: map[string]any{
			"temperature": temperature,
			"num_predict": maxTokens,
		},
	}, func(resp ollamaapi.ChatResponse) error {
		answer = resp
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("eino ollama: chat: %w", err)
	}

	calls := make([]schema.ToolCall, 0, len(answer.Message.ToolCalls))
	for i, tc := range answer.Message.ToolCalls {
		args, _ := json.Marshal(tc.Function.Arguments)
		id := tc.ID
		if id == "" {
			id = fmt.Sprintf("call_%d", i)
		}
		calls = append(calls, schema.ToolCall{
			ID:   id,
			Type: "function",
			Function: schema.FunctionCall{
				Name:      tc.Function.Name,
				Arguments: string(args),
			},
		})
	}
	return &schema.Message{
		Role:      schema.Assistant,
		Content:   answer.Message.Content,
		ToolCalls: calls,
	}, nil
}

// Stream không hỗ trợ — runner chạy non-streaming nên chỉ dùng Generate.
func (m *OllamaChatModel) Stream(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, fmt.Errorf("eino ollama: dùng Generate (chat không stream)")
}

// mapMessages đổi schema.Message sang message Ollama — giữ nguyên tool_calls
// của lượt assistant và tool_call_id/tool_name của kết quả tool để Ollama
// hiểu vòng gọi tool nhiều lượt. Mất 2 trường này là loop agent gãy ở vòng 2.
func mapMessages(input []*schema.Message) []ollamaapi.Message {
	msgs := make([]ollamaapi.Message, 0, len(input))
	for _, m := range input {
		if m == nil {
			continue
		}
		role := "user"
		switch m.Role {
		case schema.System:
			role = "system"
		case schema.Assistant:
			role = "assistant"
		case schema.Tool:
			role = "tool"
		}
		out := ollamaapi.Message{Role: role, Content: m.Content}
		for _, tc := range m.ToolCalls {
			var args ollamaapi.ToolCallFunctionArguments
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = ollamaapi.NewToolCallFunctionArguments()
			}
			out.ToolCalls = append(out.ToolCalls, ollamaapi.ToolCall{
				ID:       tc.ID,
				Function: ollamaapi.ToolCallFunction{Name: tc.Function.Name, Arguments: args},
			})
		}
		if m.Role == schema.Tool {
			out.ToolCallID = m.ToolCallID
			out.ToolName = m.ToolName
		}
		msgs = append(msgs, out)
	}
	return msgs
}

// mapTools đổi ToolInfo Eino sang function tools Ollama — parameters là JSON
// schema từ InferTool nên unmarshal thẳng vào kiểu lib. Rỗng thì nil để
// Ollama chạy chat thuần.
func mapTools(infos []*schema.ToolInfo) []ollamaapi.Tool {
	out := make([]ollamaapi.Tool, 0, len(infos))
	for _, ti := range infos {
		if ti == nil || strings.TrimSpace(ti.Name) == "" {
			continue
		}
		t := ollamaapi.Tool{Type: "function"}
		t.Function.Name = ti.Name
		t.Function.Description = ti.Desc
		if js, err := ti.ParamsOneOf.ToJSONSchema(); err == nil && js != nil {
			if raw, err := json.Marshal(js); err == nil {
				var params ollamaapi.ToolFunctionParameters
				if json.Unmarshal(raw, &params) == nil && params.Type != "" {
					t.Function.Parameters = params
				}
			}
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
