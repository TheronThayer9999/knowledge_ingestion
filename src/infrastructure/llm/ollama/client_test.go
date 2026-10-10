package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"
)

// fakeOllama giả 2 endpoint client dùng: GET /api/tags, POST /api/chat.
func fakeOllama(t *testing.T, gotModel *string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{{"name": "qwen2.5:7b"}, {"name": "llama3.1:8b"}},
		})
	})
	mux.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Model    string               `json:"model"`
			Messages []domain.ChatMessage `json:"messages"`
			Stream   bool                 `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Errorf("decode chat request: %v", err)
		}
		*gotModel = in.Model
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{"content": "xin chào"},
		})
	})
	return httptest.NewServer(mux)
}

func TestListModels(t *testing.T) {
	var gotModel string
	srv := fakeOllama(t, &gotModel)
	defer srv.Close()

	c, err := New(config.LLMConfig{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	models, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 || models[0] != "qwen2.5:7b" || models[1] != "llama3.1:8b" {
		t.Fatalf("models = %v, want [qwen2.5:7b llama3.1:8b]", models)
	}
}

func TestChatUsesRequestModel(t *testing.T) {
	var gotModel string
	srv := fakeOllama(t, &gotModel)
	defer srv.Close()

	c, err := New(config.LLMConfig{BaseURL: srv.URL, Temperature: 0.5, MaxTokens: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	out, err := c.Chat(context.Background(),
		[]domain.ChatMessage{{Role: "user", Content: "chào"}},
		domain.ChatOptions{Model: "llama3.1:8b"})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if out != "xin chào" {
		t.Fatalf("out = %q, want %q", out, "xin chào")
	}
	// Model phải là thứ user truyền theo request, không phải default nào khác.
	if gotModel != "llama3.1:8b" {
		t.Fatalf("model sent = %q, want llama3.1:8b", gotModel)
	}
}

func TestChatRequiresModel(t *testing.T) {
	var gotModel string
	srv := fakeOllama(t, &gotModel)
	defer srv.Close()

	c, err := New(config.LLMConfig{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Chat(context.Background(),
		[]domain.ChatMessage{{Role: "user", Content: "chào"}},
		domain.ChatOptions{}); err == nil {
		t.Fatal("expected error for empty model, got nil")
	}
}
