package services

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	stderrors "errors"
	"net/http"
	"testing"
	"time"

	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/middlewares"
)

// cryptoFixture dựng service với store in-memory — test không cần Redis, còn
// production đi qua NewChatCryptoService để chọn store theo config.
func cryptoFixture(mode string, ttl time.Duration) IChatCryptoService {
	return newChatCryptoService(mode, ttl, newMemorySessionStore(), middlewares.NewCurrentUser())
}

// clientHandshake mô phỏng đúng những gì FE làm: sinh cặp X25519, gọi handshake,
// rồi TỰ dẫn xuất khoá từ public key server trả về. Trả cả khoá phía client lẫn
// khoá server tra được từ store — test so sánh hai cái để chứng minh hai phía
// tính giống nhau qua toàn bộ đường đi thật (không phải từng hàm rời).
func clientHandshake(t *testing.T, svc IChatCryptoService, userID int64) (
	resp *dtos.CryptoHandshakeResponse, clientKey, serverKey []byte) {
	t.Helper()

	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("sinh khoá client: %v", err)
	}
	ctx := middlewares.WithUserID(context.Background(), userID)
	res := svc.Handshake(ctx, &dtos.CryptoHandshakeRequest{
		ClientPublic: base64.StdEncoding.EncodeToString(private.PublicKey().Bytes()),
	})
	if res.Err != nil {
		t.Fatalf("Handshake: %v", res.Err)
	}
	resp = res.Data

	raw, err := base64.StdEncoding.DecodeString(resp.ServerPublic)
	if err != nil {
		t.Fatalf("server_public không phải base64: %v", err)
	}
	peer, err := ecdh.X25519().NewPublicKey(raw)
	if err != nil {
		t.Fatalf("server_public không phải khoá X25519: %v", err)
	}
	secret, err := private.ECDH(peer)
	if err != nil {
		t.Fatalf("ECDH phía client: %v", err)
	}
	clientKey, err = utils.DeriveKey(secret, resp.SessionID)
	if err != nil {
		t.Fatalf("DeriveKey phía client: %v", err)
	}
	serverKey, ok := svc.SessionKey(ctx, resp.SessionID)
	if !ok {
		t.Fatal("SessionKey phải tra được vừa mở phiên")
	}
	return resp, clientKey, serverKey
}

// TestHandshakeTwoSidesAgree: khoá client tự tính và khoá server lưu phải giống
// hệt — nếu không, mọi phong bì FE gửi xuống sẽ Open lỗi và triệu chứng là
// "chat chết toàn bộ" rất khó truy.
func TestHandshakeTwoSidesAgree(t *testing.T) {
	svc := cryptoFixture(config.ChatCryptoOptional, time.Hour)
	resp, clientKey, serverKey := clientHandshake(t, svc, 7)

	if resp.SessionID == "" || len(resp.SessionID) != sessionIDLen {
		t.Errorf("session_id = %q, want %d ký tự hex", resp.SessionID, sessionIDLen)
	}
	if resp.Mode != config.ChatCryptoOptional {
		t.Errorf("mode = %q, want optional", resp.Mode)
	}
	if resp.ExpiresAt == "" {
		t.Error("expires_at rỗng — FE không biết khi nào phải handshake lại")
	}
	if _, err := time.Parse(time.RFC3339, resp.ExpiresAt); err != nil {
		t.Errorf("expires_at không phải RFC3339: %q", resp.ExpiresAt)
	}
	if len(serverKey) != 32 {
		t.Fatalf("khoá phiên = %d byte, want 32", len(serverKey))
	}
	if string(clientKey) != string(serverKey) {
		t.Fatal("client và server dẫn xuất ra hai khoá khác nhau")
	}
}

// TestHandshakeOffMode: mode off thì không dựng phiên (Redis không bị nhét rác
// vô ích) và FE nhận về mode để đi đường plaintext.
func TestHandshakeOffMode(t *testing.T) {
	svc := cryptoFixture(config.ChatCryptoOff, time.Hour)
	res := svc.Handshake(middlewares.WithUserID(context.Background(), 7),
		&dtos.CryptoHandshakeRequest{ClientPublic: "AAA"})
	if res.Err != nil {
		t.Fatalf("Handshake: %v", res.Err)
	}
	if res.Data.SessionID != "" || res.Data.Mode != config.ChatCryptoOff {
		t.Errorf("mode off phải trả phiên rỗng, got %+v", res.Data)
	}
}

func TestHandshakeRejectsBadInput(t *testing.T) {
	svc := cryptoFixture(config.ChatCryptoOptional, time.Hour)

	// Không có user trong ctx = route quên gắn auth.
	res := svc.Handshake(context.Background(), &dtos.CryptoHandshakeRequest{ClientPublic: "AAA"})
	if httpCode(t, res.Err) != http.StatusBadRequest {
		t.Errorf("thiếu user: status = %d, want 400", httpCode(t, res.Err))
	}

	// Public key rác phải ra 400 với message đã gọt — không phải 500, và không
	// được leak chi tiết thuật toán ra client.
	bad := []string{"", "!!!!", base64.StdEncoding.EncodeToString([]byte("quá ngắn"))}
	for _, value := range bad {
		res := svc.Handshake(middlewares.WithUserID(context.Background(), 7),
			&dtos.CryptoHandshakeRequest{ClientPublic: value})
		e := errors.From(res.Err)
		if e == nil || e.GetHttpCode() != http.StatusBadRequest {
			t.Errorf("client_public %q: err = %v, want 400", value, res.Err)
			continue
		}
		if e.Message != "client_public không phải khoá X25519 hợp lệ" {
			t.Errorf("message rò chi tiết crypto: %q", e.Message)
		}
	}
}

// TestSessionKeyIsBoundToOwner: sid lấy từ log/tab của user khác không mở được
// khoá của chủ thật — phải coi như stale.
func TestSessionKeyIsBoundToOwner(t *testing.T) {
	svc := cryptoFixture(config.ChatCryptoOptional, time.Hour)
	resp, _, _ := clientHandshake(t, svc, 7)

	if _, ok := svc.SessionKey(middlewares.WithUserID(context.Background(), 8), resp.SessionID); ok {
		t.Error("user 8 dùng sid của user 7 mà vẫn có khoá")
	}
	// ctx không identity (route ngoài group auth) cũng phải rớt.
	if _, ok := svc.SessionKey(context.Background(), resp.SessionID); ok {
		t.Error("ctx không có user_id mà vẫn tra được khoá")
	}
}

// TestSessionKeyRejectsGarbageAndExpiry: sid sai khuôn không xuống store; phiên
// hết TTL coi như không tồn tại (client sẽ handshake lại).
func TestSessionKeyRejectsGarbageAndExpiry(t *testing.T) {
	svc := cryptoFixture(config.ChatCryptoOptional, time.Hour)
	ctx := middlewares.WithUserID(context.Background(), 7)

	for _, sid := range []string{"", "ngắn", "ZZ02030405060708090a0b0c0d0e0f10",
		"0102030405060708090a0b0c0d0e0f1"} {
		if _, ok := svc.SessionKey(ctx, sid); ok {
			t.Errorf("sid %q phải bị từ chối", sid)
		}
	}

	// TTL đủ dài để lượt đọc ngay sau handshake chắc chắn còn hiệu lực (đồng
	// hồ máy thô ~15ms, một tick rơi giữa lúc ghi và lúc đọc là phiên chết
	// oan), rồi ngủ LÂU HƠN TTL để chắc chắn đã hết hạn. TTL 1ns là mẹo chỉ
	// đúng trên đồng hồ ns — trên máy đồng hồ thô nó fail, trên Linux CI nó
	// fail luôn (đọc sau ghi vài µs là đã quá hạn).
	short := cryptoFixture(config.ChatCryptoOptional, 200*time.Millisecond)
	resp, _, _ := clientHandshake(t, short, 7)
	time.Sleep(500 * time.Millisecond)
	if _, ok := short.SessionKey(ctx, resp.SessionID); ok {
		t.Error("phiên hết hạn vẫn tra được khoá")
	}
}

// TestSessionRoundTripThroughStore: khoá lấy từ store phải mở được phong bì do
// client dựng, và mở sai hướng/sai sid phải fail — đây là chuỗi bước middleware
// sẽ chạy, test ở tầng service để middleware gọn.
func TestSessionRoundTripThroughStore(t *testing.T) {
	svc := cryptoFixture(config.ChatCryptoOptional, time.Hour)
	resp, clientKey, serverKey := clientHandshake(t, svc, 7)

	body := []byte(`{"message":"Phụ cấp công tác là bao nhiêu?"}`)
	nonce, ct, err := utils.Seal(clientKey, utils.AAD(resp.SessionID, utils.DirRequest), body)
	if err != nil {
		t.Fatalf("client seal: %v", err)
	}
	plain, err := utils.Open(serverKey, utils.AAD(resp.SessionID, utils.DirRequest), nonce, ct)
	if err != nil {
		t.Fatalf("server open: %v", err)
	}
	if string(plain) != string(body) {
		t.Fatalf("plain = %q", plain)
	}
	if _, err := utils.Open(serverKey, utils.AAD(resp.SessionID, utils.DirResponse), nonce, ct); !stderrors.Is(err, utils.ErrEnvelopeBad) {
		t.Error("phong bì chiều request mở được bằng AAD chiều response")
	}
}

// TestSessionIDNotGuessableBetweenUsers: hai phiên của cùng một user phải khác
// sid và khác khoá — không có "trạng thái toàn cục" dùng chung.
func TestSessionIDNotGuessableBetweenUsers(t *testing.T) {
	svc := cryptoFixture(config.ChatCryptoOptional, time.Hour)
	first, _, firstKey := clientHandshake(t, svc, 7)
	second, _, secondKey := clientHandshake(t, svc, 7)

	if first.SessionID == second.SessionID {
		t.Fatal("hai handshake trả cùng một sid")
	}
	if string(firstKey) == string(secondKey) {
		t.Fatal("hai handshake dùng chung một khoá phiên")
	}
}

func httpCode(t *testing.T, err error) int {
	t.Helper()
	e := errors.From(err)
	if e == nil {
		if err == nil {
			t.Fatal("muốn lỗi, nhận được nil")
		}
		return 0
	}
	return e.GetHttpCode()
}
