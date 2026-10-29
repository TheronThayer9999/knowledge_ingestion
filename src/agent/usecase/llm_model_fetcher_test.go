package usecase

import (
	"context"
	stderrors "errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"

	apperrors "knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/config"
)

// TestParseModelList nhận cả 2 khuôn: OpenAI {"data":[{"id"}]} và Ollama
// {"models":[{"name"}]}; kết quả sắp A→Z và dedup để popup ổn định.
func TestParseModelList(t *testing.T) {
	openaiBody := []byte(`{"object":"list","data":[{"id":"b"},{"id":"a"},{"id":"a"},{"id":""}]}`)
	models, err := parseModelList(config.LLMTypeOpenAI, openaiBody)
	if err != nil {
		t.Fatalf("openai: %v", err)
	}
	if strings.Join(models, ",") != "a,b" {
		t.Errorf("openai models = %v, muốn [a b]", models)
	}

	ollamaBody := []byte(`{"models":[{"name":"gemma4:31b"},{"name":"llama3.1:8b"}]}`)
	models, err = parseModelList(config.LLMTypeOllama, ollamaBody)
	if err != nil {
		t.Fatalf("ollama: %v", err)
	}
	if strings.Join(models, ",") != "gemma4:31b,llama3.1:8b" {
		t.Errorf("ollama models = %v", models)
	}

	if _, err := parseModelList(config.LLMTypeOpenAI, []byte(`{"data":[]}`)); err == nil {
		t.Error("danh sách rỗng phải là lỗi đọc được, không phải 200 rỗng")
	}
	if _, err := parseModelList(config.LLMTypeOpenAI, []byte(`<html>`)); err == nil {
		t.Error("body không phải JSON phải lỗi")
	}
}

// TestModelsEndpoints: Ollama đi /api/tags sau khi cắt hậu tố /api khỏi
// base_url; OpenAI-compatible thiếu key thì báo ngay, khỏi gọi rồi nhận 401.
// Base thiếu "/v1" thì thêm ứng viên /v1 và /api/v1 — người dùng hay dán
// URL không đủ phần version.
func TestModelsEndpoints(t *testing.T) {
	eps, err := modelsEndpoints(config.LLMProviderConfig{
		Type: config.LLMTypeOllama, BaseURL: "http://localhost:11434/api/",
	})
	if err != nil || len(eps) != 1 || eps[0].url != "http://localhost:11434/api/tags" {
		t.Errorf("ollama endpoints = %+v, err = %v", eps, err)
	}

	eps, err = modelsEndpoints(config.LLMProviderConfig{
		Type: config.LLMTypeOpenAI, BaseURL: "https://api.groq.com/openai/v1/", APIKey: "k",
	})
	if err != nil || len(eps) != 1 || eps[0].url != "https://api.groq.com/openai/v1/models" {
		t.Errorf("openai endpoints = %+v, err = %v", eps, err)
	}
	if eps[0].base != "https://api.groq.com/openai/v1" {
		t.Errorf("base ứng viên = %q", eps[0].base)
	}

	eps, err = modelsEndpoints(config.LLMProviderConfig{
		Type: config.LLMTypeOpenAI, BaseURL: "https://api.groq.com/openai", APIKey: "k",
	})
	if err != nil || len(eps) != 3 {
		t.Fatalf("base thiếu /v1 phải có 3 ứng viên, nhận %d (%v)", len(eps), err)
	}
	if eps[1].url != "https://api.groq.com/openai/v1/models" ||
		eps[1].base != "https://api.groq.com/openai/v1" {
		t.Errorf("ứng viên /v1 = %+v", eps[1])
	}
	if eps[2].url != "https://api.groq.com/openai/api/v1/models" {
		t.Errorf("ứng viên /api/v1 = %+v", eps[2])
	}

	if _, err := modelsEndpoints(config.LLMProviderConfig{
		Type: config.LLMTypeOpenAI, BaseURL: "https://x/v1",
	}); err == nil {
		t.Error("openai thiếu key phải lỗi trước khi gọi")
	}
}

// TestModelFetcherFallsBackToV1: base user dán thiếu "/v1" thì phải dò ra
// endpoint thật và TRẢ LẠI base đúng (để lưu vào DB, client chat dựng đúng
// base đó) — không phải báo lỗi 404.
func TestModelFetcherFallsBackToV1(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path != "/openai/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	}))
	defer srv.Close()

	fetcher := newLLMModelFetcher(srv.Client())
	list, err := fetcher.Fetch(context.Background(), config.LLMProviderConfig{
		Type: config.LLMTypeOpenAI, BaseURL: srv.URL + "/openai", APIKey: "k",
	})
	if err != nil {
		t.Fatalf("dò model lỗi: %v", err)
	}
	if len(list.Models) != 1 || list.Models[0] != "m1" {
		t.Errorf("models = %v, muốn [m1]", list.Models)
	}
	if list.BaseURL != srv.URL+"/openai/v1" {
		t.Errorf("base = %q, muốn %q", list.BaseURL, srv.URL+"/openai/v1")
	}
	if len(paths) != 2 || paths[0] != "/openai/models" || paths[1] != "/openai/v1/models" {
		t.Errorf("đường dẫn đã gọi = %v", paths)
	}
}

// TestModelFetcherStopsOn401: sai key là lỗi THẬT — không đổi URL ứng viên khác
// (đổi URL không giúp, chỉ tốn thêm request vào provider).
func TestModelFetcherStopsOn401(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	fetcher := newLLMModelFetcher(srv.Client())
	_, err := fetcher.Fetch(context.Background(), config.LLMProviderConfig{
		Type: config.LLMTypeOpenAI, BaseURL: srv.URL, APIKey: "sai",
	})
	if err == nil {
		t.Fatal("sai key phải báo lỗi")
	}
	if calls != 1 {
		t.Errorf("số request = %d, muốn 1 (401 thì dừng luôn)", calls)
	}
	if !strings.Contains(err.Error(), "key") {
		t.Errorf("lỗi = %q, muốn nói rõ là key", err.Error())
	}
}

// fakeTimeoutError là net.Error tự dựng để test nhánh "quá giờ" mà không phải
// chờ đúng 20 giây thật.
type fakeTimeoutError struct{}

func (fakeTimeoutError) Error() string   { return "dial tcp: fake timeout" }
func (fakeTimeoutError) Timeout() bool   { return true }
func (fakeTimeoutError) Temporary() bool { return true }

// TestModelFetcherTransportError khoá chốt E2E từng phát hiện: "Tải model" với
// provider không tới được trả 500 "internal server error". Giờ phải là
// llmFetchError (WrapError bọc thành 502) kèm lý do admin sửa được, và lý do
// KHÔNG chứa host/cổng của hạ tầng nội bộ.
func TestModelFetcherTransportError(t *testing.T) {
	const host = "api.nha-cung-cap-kia.invalid"
	fetcher := newLLMModelFetcher(nil)
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"tên miền không tồn tại", &url.Error{Op: "Get", URL: "https://" + host + "/v1/models",
			Err: &net.DNSError{Err: "no such host", Name: host}}, "tên miền"},
		{"provider treo", &url.Error{URL: "https://" + host + "/v1/models", Err: fakeTimeoutError{}},
			"20 giây"},
		{"cổng bị từ chối", &url.Error{URL: "https://" + host + "/v1/models", Err: &net.OpError{
			Op: "dial", Net: "tcp", Addr: &net.TCPAddr{Port: 20128},
			Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}}}, "lắng nghe"},
	}
	for _, tc := range cases {
		got := fetcher.transportError(context.Background(), tc.err)
		var pe *llmFetchError
		if !stderrors.As(got, &pe) {
			t.Errorf("%s: phải bọc thành llmFetchError, nhận %T (%v)", tc.name, got, got)
			continue
		}
		if !strings.Contains(pe.msg, tc.want) {
			t.Errorf("%s: lý do = %q, muốn chứa %q", tc.name, pe.msg, tc.want)
		}
		if strings.Contains(pe.msg, host) || strings.Contains(pe.msg, "20128") {
			t.Errorf("%s: lộ hạ tầng nội bộ: %q", tc.name, pe.msg)
		}
	}
}

// TestModelFetcherWrapErrorMapsTransportToBadGateway: lý do đã lọc phải đi kèm
// 502 — admin phân biệt "provider không trả lời" (502) với "server mình hỏng" (500).
func TestModelFetcherWrapErrorMapsTransportToBadGateway(t *testing.T) {
	fetcher := newLLMModelFetcher(nil)
	raw := fetcher.transportError(context.Background(),
		&net.DNSError{Err: "no such host", Name: "api.x.invalid"})
	mapped := apperrors.From(fetcher.WrapError("groq", raw))
	if mapped == nil || mapped.HttpCode != http.StatusBadGateway {
		t.Fatalf("muốn 502, nhận %+v", mapped)
	}
	if !strings.Contains(mapped.Message, "groq") || !strings.Contains(mapped.Message, "tên miền") {
		t.Errorf("message = %q, muốn tên provider + lý do", mapped.Message)
	}
}

// TestModelFetcherKeepsContextCancel: admin đóng popup giữa lúc chờ là ctx chết,
// không phải provider hỏng — để nguyên lỗi gốc để Render map thành 499 thay vì
// báo nhầm "nhà cung cấp không trả lời".
func TestModelFetcherKeepsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dns := &net.DNSError{Err: "no such host", Name: "api.x.invalid"}
	got := newLLMModelFetcher(nil).transportError(ctx, dns)
	var pe *llmFetchError
	if stderrors.As(got, &pe) {
		t.Errorf("ctx bị hủy mà bọc thành lỗi provider: %q", pe.msg)
	}
	if got != dns {
		t.Errorf("phải trả nguyên lỗi gốc, nhận %v", got)
	}
}
