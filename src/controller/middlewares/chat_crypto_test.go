package middlewares

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/dtos"

	"github.com/gin-gonic/gin"
)

// fakeCryptoSessions dựng đúng những gì middleware gọi: mode + tra khoá. Khoá
// test là byte tự đặt — thuật toán thật đã có test riêng ở utils/services.
type fakeCryptoSessions struct {
	mode  string
	keys  map[string][]byte
	uid   int64
	calls []string
}

func newFakeCryptoSessions(mode string) (*fakeCryptoSessions, string, []byte) {
	sid, err := utils.NewSessionID()
	if err != nil {
		panic(err)
	}
	key := bytes.Repeat([]byte{0x2b}, 32)
	return &fakeCryptoSessions{mode: mode, keys: map[string][]byte{sid: key}, uid: 7}, sid, key
}

func (f *fakeCryptoSessions) Mode() string { return f.mode }

func (f *fakeCryptoSessions) SessionKey(_ context.Context, sid string) ([]byte, bool) {
	f.calls = append(f.calls, sid)
	key, ok := f.keys[sid]
	return key, ok
}

// cryptoRouter dựng engine có chain Protected(auth, crypto) nhưng bỏ qua auth
// thật: middleware test đọc user qua header X-Test-User do chính engine nhét
// vào ctx, để tách lớp mã hoá khỏi lớp JWT.
func cryptoRouter(sessions ICryptoSessions, echo gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := engine.Group("/api/v1")
	group.Use(sessions.(*fakeCryptoSessions).authStub())
	group.Use(NewChatCryptoMiddleware(sessions).Handler())
	group.POST("/chat", echo)
	group.GET("/conversations", echo)
	group.POST("/chat/attachments", echo)
	group.POST("/crypto/handshake", echo)
	group.POST("/users", echo)
	return engine
}

// authStub là bản replace của auth.Handler cho test: chỉ nhét user_id vào
// request context đúng như bản thật làm.
func (f *fakeCryptoSessions) authStub() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(WithUserID(c.Request.Context(), f.uid))
		c.Next()
	}
}

// echoBody là handler nghiệp vụ giả: nói lại những gì nó NHẬN được để test kiểm
// tra middleware đã mở phong bì trước khi handler chạy, rồi trả JSON như service thật.
func echoBody(c *gin.Context) {
	raw, _ := io.ReadAll(c.Request.Body)
	c.JSON(http.StatusOK, dtos.ResponseResource{
		Status: true, Code: 200,
		Message: string(raw),
	})
}

func sealRequest(t *testing.T, key []byte, sid string, plain string) string {
	t.Helper()
	nonce, ct, err := utils.Seal(key, utils.AAD(sid, utils.DirRequest), []byte(plain))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	raw, err := json.Marshal(dtos.CryptoEnvelope{V: 1, SID: sid, Nonce: nonce, CT: ct})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return string(raw)
}

func openResponse(t *testing.T, key []byte, body string) string {
	t.Helper()
	var env dtos.CryptoEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("response không phải phong bì: %v (%q)", err, body)
	}
	plain, err := utils.Open(key, utils.AAD(env.SID, utils.DirResponse), env.Nonce, env.CT)
	if err != nil {
		t.Fatalf("open response: %v", err)
	}
	return string(plain)
}

func do(engine http.Handler, method, path string, body string, headers map[string]string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// TestCryptoPlaintextStillWorksWhenOptional: đường rollout — client chưa nâng
// cấp gửi plaintext thì vẫn chạy, response cũng plaintext. Đây là điều kiện để
// BE deploy trước FE mà không sập.
func TestCryptoPlaintextStillWorksWhenOptional(t *testing.T) {
	sessions, _, _ := newFakeCryptoSessions(config.ChatCryptoOptional)
	engine := cryptoRouter(sessions, echoBody)

	rec := do(engine, http.MethodPost, "/api/v1/chat", `{"message":"xin chào"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"nonce"`) {
		t.Fatal("client plaintext bị niêm phong response làm nó không đọc được")
	}
	var resp dtos.ResponseResource
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Message != `{"message":"xin chào"}` {
		t.Errorf("handler nhận = %q", resp.Message)
	}
}

// TestCryptoRoundTrip: client có phiên thì body xuống handler là plaintext đã
// mở, còn response lên client là phong bì mở được bằng đúng khoá phiên.
func TestCryptoRoundTrip(t *testing.T) {
	sessions, sid, key := newFakeCryptoSessions(config.ChatCryptoOptional)
	engine := cryptoRouter(sessions, echoBody)

	plain := `{"message":"Phụ cấp công tác?"}`
	rec := do(engine, http.MethodPost, "/api/v1/chat",
		sealRequest(t, key, sid, plain), map[string]string{SessionHeader: sid})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	opened := openResponse(t, key, rec.Body.String())
	var resp dtos.ResponseResource
	if err := json.Unmarshal([]byte(opened), &resp); err != nil {
		t.Fatalf("body đã mở không phải JSON nghiệp vụ: %v (%q)", err, opened)
	}
	// Message là nguyên văn body handler đọc được: phải là plaintext gốc, không
	// phải phong bì — chứng minh middleware mở TRƯỚC khi Bind chạy.
	if resp.Message != plain {
		t.Fatalf("handler nhận sai: %q", resp.Message)
	}
}

// TestCryptoGetHasNoBodyButSealedResponse: GET không body nhưng vẫn phải được
// niêm phong — lịch sử hội thoại là dữ liệu cần bảo vệ như câu hỏi.
func TestCryptoGetSealsResponseWithoutRequestBody(t *testing.T) {
	sessions, sid, key := newFakeCryptoSessions(config.ChatCryptoOptional)
	engine := cryptoRouter(sessions, echoBody)

	rec := do(engine, http.MethodGet, "/api/v1/conversations", "", map[string]string{SessionHeader: sid})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	opened := openResponse(t, key, rec.Body.String())
	if !strings.Contains(opened, "200") {
		t.Fatalf("response mở ra lạ: %q", opened)
	}
}

// TestCryptoMultipartBodyUntouched: /chat/attachments upload tệp — body là
// multipart, không phải JSON nên middleware phải để nguyên (chỉ niêm phong
// response). Test này khoá quyết định "tệp đi plaintext, kết quả trích bị khoá".
func TestCryptoMultipartBodyUntouched(t *testing.T) {
	sessions, sid, key := newFakeCryptoSessions(config.ChatCryptoOptional)
	engine := cryptoRouter(sessions, echoBody)

	boundary := "ki-test"
	body := "--" + boundary + "\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.txt\"" +
		"\r\n\r\nnoi dung tho\r\n--" + boundary + "--\r\n"
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/attachments", strings.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	req.Header.Set(SessionHeader, sid)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	opened := openResponse(t, key, rec.Body.String())
	if !strings.Contains(opened, "noi dung tho") {
		t.Errorf("handler không đọc được tệp multipart: %q", opened)
	}
}

// TestCryptoStaleSession: sid lạ/hết hạn → 400 + header stale, TUYỆT ĐỐI không
// 401 (401 đẩy FE vào luồng refresh token, ở đây token vẫn còn hạn) và không
// được để handler chạy.
func TestCryptoStaleSession(t *testing.T) {
	sessions, sid, key := newFakeCryptoSessions(config.ChatCryptoOptional)
	engine := cryptoRouter(sessions, echoBody)

	rec := do(engine, http.MethodPost, "/api/v1/chat",
		sealRequest(t, key, sid, `{"message":"xin chào"}`),
		map[string]string{SessionHeader: "00000000000000000000000000000000"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, muốn 400", rec.Code)
	}
	if rec.Header().Get(SessionHeader) != SessionStaleValue {
		t.Errorf("thiếu header stale: %v", rec.Header())
	}
	// Chuỗi "xin chào" chỉ xuất hiện nếu handler chạy — message của lỗi stale
	// cũng nằm trong field message nên không được soi khoá đó.
	if strings.Contains(rec.Body.String(), "xin chào") {
		t.Errorf("sid hỏng mà handler vẫn chạy: %s", rec.Body.String())
	}
}

// TestCryptoRequiredModeRejectsPlaintext: sau khi FE xong, bật required thì
// plaintext bị chặn — cờ này là lý do mode nằm trong config.
func TestCryptoRequiredModeRejectsPlaintext(t *testing.T) {
	sessions, sid, key := newFakeCryptoSessions(config.ChatCryptoRequired)
	engine := cryptoRouter(sessions, echoBody)

	rec := do(engine, http.MethodPost, "/api/v1/chat", `{"message":"x"}`, nil)
	if rec.Code != http.StatusBadRequest || rec.Header().Get(SessionHeader) != SessionStaleValue {
		t.Fatalf("mode required mà plaintext vẫn qua: %d %v", rec.Code, rec.Header())
	}
	// Có phiên thì vẫn chạy bình thường.
	ok := do(engine, http.MethodPost, "/api/v1/chat",
		sealRequest(t, key, sid, `{"message":"x"}`), map[string]string{SessionHeader: sid})
	if ok.Code != http.StatusOK {
		t.Fatalf("mode required chặn cả client đã handshake: %d", ok.Code)
	}
}

// TestCryptoHandshakeRouteIsNeverEncrypted: chính route dựng khoá không được bị
// lớp mã hoá chạm tới, nếu không sẽ không bao giờ mở được phiên.
func TestCryptoHandshakeRouteIsNeverEncrypted(t *testing.T) {
	sessions, _, _ := newFakeCryptoSessions(config.ChatCryptoRequired)
	engine := cryptoRouter(sessions, echoBody)

	body := `{"client_public":"AAAA"}`
	rec := do(engine, http.MethodPost, "/api/v1/crypto/handshake", body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("handshake bị chặn: %d %s", rec.Code, rec.Body.String())
	}
	var resp dtos.ResponseResource
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("handshake trả phong bì chứ không plaintext: %v", err)
	}
	if resp.Message != body {
		t.Errorf("handler nhận = %q", resp.Message)
	}
}

// TestCryptoScopeOnlyChat: các API khác (upload, admin, llm) giữ nguyên như cũ;
// /users không nằm trong phạm vi "khách hỏi AI".
func TestCryptoScopeOnlyChat(t *testing.T) {
	sessions, _, _ := newFakeCryptoSessions(config.ChatCryptoRequired)
	engine := cryptoRouter(sessions, echoBody)

	rec := do(engine, http.MethodPost, "/api/v1/users", `{"name":"khong phan vi"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("route ngoài phạm vi bị chặn: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"nonce"`) {
		t.Error("route ngoài phạm vi lại bị niêm phong response")
	}
}

// TestCryptoEnvelopeSidMustMatchHeader: phòng bì ghi sid của phiên khác với
// header — hoặc client nhầm, hoặc ai đó cố đổi hướng dữ liệu giữa hai phiên.
func TestCryptoEnvelopeSidMustMatchHeader(t *testing.T) {
	sessions, sid, key := newFakeCryptoSessions(config.ChatCryptoOptional)
	engine := cryptoRouter(sessions, echoBody)

	other, err := utils.NewSessionID()
	if err != nil {
		t.Fatalf("NewSessionID: %v", err)
	}
	rec := do(engine, http.MethodPost, "/api/v1/chat",
		sealRequest(t, key, other, `{"message":"x"}`), map[string]string{SessionHeader: sid})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, muốn 400", rec.Code)
	}
}

// TestCryptoBadEnvelopeFailsClosed: ciphertext không mở được (khác khoá) phải
// 400, handler không chạy, và plaintext tuyệt đối không xuống dây.
func TestCryptoBadEnvelopeFailsClosed(t *testing.T) {
	sessions, sid, _ := newFakeCryptoSessions(config.ChatCryptoOptional)
	engine := cryptoRouter(sessions, echoBody)

	wrongKey := bytes.Repeat([]byte{0x11}, 32)
	rec := do(engine, http.MethodPost, "/api/v1/chat",
		sealRequest(t, wrongKey, sid, `{"message":"x"}`), map[string]string{SessionHeader: sid})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, muốn 400", rec.Code)
	}
}

// TestCryptoHandlerErrorAlsoSealed: 400/500 do chính handler trả cũng đi qua
// cùng một lớp niêm phong — FE chỉ có một đường đọc response.
func TestCryptoHandlerErrorAlsoSealed(t *testing.T) {
	sessions, sid, key := newFakeCryptoSessions(config.ChatCryptoOptional)
	engine := cryptoRouter(sessions, func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusUnprocessableEntity,
			dtos.ResponseResource{Status: false, Code: 422, Message: "nghiệp vụ từ chối"})
	})

	rec := do(engine, http.MethodPost, "/api/v1/chat",
		sealRequest(t, key, sid, `{"message":"x"}`), map[string]string{SessionHeader: sid})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, muốn giữ nguyên 422 của handler", rec.Code)
	}
	if got := openResponse(t, key, rec.Body.String()); !strings.Contains(got, "nghiệp vụ từ chối") {
		t.Errorf("response mở ra = %q", got)
	}
}

// TestCryptoOffModePassesEverything: mode off là tắt hẳn lớp này (không tra
// store, không đụng body) — đường lùi khi có sự cố.
func TestCryptoOffModePassesEverything(t *testing.T) {
	sessions, sid, key := newFakeCryptoSessions(config.ChatCryptoOff)
	engine := cryptoRouter(sessions, echoBody)

	rec := do(engine, http.MethodPost, "/api/v1/chat",
		sealRequest(t, key, sid, `{"message":"x"}`), map[string]string{SessionHeader: sid})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(sessions.calls) != 0 {
		t.Errorf("mode off vẫn gọi SessionKey %d lần", len(sessions.calls))
	}
	if strings.Contains(rec.Body.String(), `"nonce"`) {
		t.Error("mode off mà response vẫn bị niêm phong")
	}
}

// TestCryptoStatusPreserved: 201 của handler phải còn nguyên sau khi bọc đệm —
// niêm phong không được phép biến mọi response thành 200.
func TestCryptoStatusPreserved(t *testing.T) {
	sessions, sid, key := newFakeCryptoSessions(config.ChatCryptoOptional)
	engine := cryptoRouter(sessions, func(c *gin.Context) {
		c.JSON(http.StatusCreated, dtos.ResponseResource{Status: true, Code: 201, Message: "đã tạo"})
	})

	rec := do(engine, http.MethodPost, "/api/v1/chat",
		sealRequest(t, key, sid, `{"message":"x"}`), map[string]string{SessionHeader: sid})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, muốn 201", rec.Code)
	}
	if got := openResponse(t, key, rec.Body.String()); !strings.Contains(got, "đã tạo") {
		t.Errorf("body mở ra = %q", got)
	}
}

// TestCryptoNoBodyResponseFlushesStatus: handler trả 204 không body (DELETE) —
// không có gì để niêm phong, nhưng status vẫn phải xuống dây.
func TestCryptoNoBodyResponseFlushesStatus(t *testing.T) {
	sessions, sid, _ := newFakeCryptoSessions(config.ChatCryptoOptional)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := engine.Group("/api/v1")
	group.Use(sessions.authStub())
	group.Use(NewChatCryptoMiddleware(sessions).Handler())
	group.DELETE("/conversations/:id", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/conversations/9", nil)
	req.Header.Set(SessionHeader, sid)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, muốn 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("204 mà có body: %q", rec.Body.String())
	}
}

// TestCryptoBufferingDoesNotLeakPlaintext: khi niêm phong thất bại (khóa hỏng),
// server phải bỏ body chứ không đổ plaintext cũ xuống dây.
func TestCryptoBufferingDoesNotLeakPlaintext(t *testing.T) {
	sid, err := utils.NewSessionID()
	if err != nil {
		t.Fatalf("NewSessionID: %v", err)
	}
	sessions := &fakeCryptoSessions{
		mode: config.ChatCryptoOptional,
		// Khoá 16 byte: utils.Seal từ chối (không đủ 32) → nhánh niêm phong lỗi.
		keys: map[string][]byte{sid: bytes.Repeat([]byte{0x01}, 16)},
		uid:  7,
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	group := engine.Group("/api/v1")
	group.Use(sessions.authStub())
	group.Use(NewChatCryptoMiddleware(sessions).Handler())
	group.POST("/chat", func(c *gin.Context) {
		c.JSON(http.StatusOK, dtos.ResponseResource{Status: true, Code: 200, Message: "BÍ MẬT"})
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", strings.NewReader(`{"message":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(SessionHeader, sid)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, muốn 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "BÍ MẬT") {
		t.Fatal("plaintext rò ra dây khi niêm phong lỗi")
	}
}

// TestInScopeTable chốt phạm vi bằng bảng, thêm route mới không nhầm vào scope.
func TestInScopeTable(t *testing.T) {
	cases := map[string]bool{
		"/api/v1/chat":                      true,
		"/api/v1/chat/attachments":          true,
		"/api/v1/conversations":             true,
		"/api/v1/conversations/12":          true,
		"/api/v1/conversations/12/messages": true,
		"/api/v1/crypto/handshake":          false,
		"/api/v1/search":                    false,
		"/api/v1/llm/options":               false,
		"/api/v1/admin/queue":               false,
		"/api/v1/articles/5":                false,
		"/health":                           false,
	}
	for path, want := range cases {
		if got := inScope(path); got != want {
			t.Errorf("inScope(%q) = %v, want %v", path, got, want)
		}
	}
}
