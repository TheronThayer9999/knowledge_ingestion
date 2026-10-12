package eino

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"

	"knowledge_ingestion/src/config"
)

func testModelCfg(url string) config.LLMConfig {
	return config.LLMConfig{
		BaseURL:     url,
		Temperature: 0.1,
		MaxTokens:   512,
		TimeoutSec:  10,
	}
}

// TestGenerateToolCalls khóa hợp đồng tool-calling: Ollama trả tool_calls thì
// adapter phải parse ra schema.ToolCalls cho agent ReAct gọi tiếp.
func TestGenerateToolCalls(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"knowledge_search","arguments":{"query":"mức phạt","limit":5}}}]}}`))
	}))
	defer srv.Close()

	m, err := NewOllamaChatModel(testModelCfg(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	msg, err := m.Generate(WithModel(context.Background(), "m"),
		[]*schema.Message{schema.UserMessage("Mức phạt?")})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != "knowledge_search" {
		t.Fatalf("phải parse tool_call, got %+v", msg.ToolCalls)
	}
	if !strings.Contains(msg.ToolCalls[0].Function.Arguments, "mức phạt") {
		t.Fatalf("arguments mất query, got %q", msg.ToolCalls[0].Function.Arguments)
	}
	// Payload gửi Ollama phải kèm tools (tên + schema tham số cho model).
	tools, _ := gotBody["tools"].([]any)
	if len(tools) != 0 {
		t.Fatalf("test này không gửi tools mà server vẫn trả tool_calls (sai kịch bản), got %v", gotBody["tools"])
	}
}

// TestGenerateSendsTools khóa chiều gửi: có ToolInfo thì payload phải có
// function knowledge_search kèm parameters.query để model biết mà gọi.
func TestGenerateSendsTools(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"Chào bạn!"}}`))
	}))
	defer srv.Close()

	m, err := NewOllamaChatModel(testModelCfg(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ti, err := toolInfoForTest()
	if err != nil {
		t.Fatalf("tool info: %v", err)
	}
	msg, err := m.Generate(WithModel(context.Background(), "m"),
		[]*schema.Message{schema.UserMessage("xin chào")},
		modelWithToolsForTest(ti))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if msg.Content != "Chào bạn!" {
		t.Fatalf("content = %q", msg.Content)
	}
	raw, _ := json.Marshal(gotBody["tools"])
	if !strings.Contains(string(raw), "knowledge_search") || !strings.Contains(string(raw), "query") {
		t.Fatalf("payload tools thiếu tên/schema, got %s", raw)
	}
}

func TestGenerateRequiresModel(t *testing.T) {
	m, err := NewOllamaChatModel(testModelCfg("http://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := m.Generate(context.Background(), []*schema.Message{schema.UserMessage("x")}); err == nil {
		t.Fatal("thiếu model phải lỗi, không tự đoán")
	}
}

// toolInfoForTest dựng ToolInfo từ struct args thật (cùng đường InferTool)
// để test payload gửi Ollama khớp schema tool dùng ở agent.
func toolInfoForTest() (*schema.ToolInfo, error) {
	return toolutils.GoStruct2ToolInfo[KnowledgeSearchArgs]("knowledge_search", "test")
}

func modelWithToolsForTest(ti *schema.ToolInfo) einomodel.Option {
	return einomodel.WithTools([]*schema.ToolInfo{ti})
}
