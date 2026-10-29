package utils

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

const testSid = "0102030405060708090a0b0c0d0e0f10"

// handshakeFixture mô phỏng đúng một handshake: client tự sinh cặp khoá
// X25519 (bên FE dùng WebCrypto, ở đây dùng crypto/ecdh cho test được), server
// dùng NewE2EEKeyPair. Trả về KHOÁ TỪ HAI PHÍA riêng biệt để test chứng minh
// chúng bằng nhau — đó là chỗ dễ sai nhất của toàn bộ tính năng.
func handshakeFixture(t *testing.T) (serverKey, clientKey []byte) {
	t.Helper()

	client, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("sinh khoá client: %v", err)
	}
	server, err := NewE2EEKeyPair()
	if err != nil {
		t.Fatalf("NewE2EEKeyPair: %v", err)
	}
	clientB64 := base64.StdEncoding.EncodeToString(client.PublicKey().Bytes())

	serverKey, err = server.SharedKey(clientB64, testSid)
	if err != nil {
		t.Fatalf("SharedKey phía server: %v", err)
	}
	// Phía client: ECDH ngược chiều rồi dẫn xuất bằng đúng thông số.
	secret, err := client.ECDH(serverRaw(t, server))
	if err != nil {
		t.Fatalf("ECDH phía client: %v", err)
	}
	clientKey, err = DeriveKey(secret, testSid)
	if err != nil {
		t.Fatalf("DeriveKey phía client: %v", err)
	}
	return serverKey, clientKey
}

func serverRaw(t *testing.T, k *E2EEKeyPair) *ecdh.PublicKey {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(k.PublicKeyB64())
	if err != nil {
		t.Fatalf("giải mã public key server: %v", err)
	}
	peer, err := ecdh.X25519().NewPublicKey(raw)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}
	return peer
}

// TestHandshakeDerivesSameKeyBothSides: hai phía phải ra cùng một khoá 32 byte.
func TestHandshakeDerivesSameKeyBothSides(t *testing.T) {
	serverKey, clientKey := handshakeFixture(t)
	if len(serverKey) != 32 {
		t.Fatalf("khoá phiên phải 32 byte, nhận %d", len(serverKey))
	}
	if !bytes.Equal(serverKey, clientKey) {
		t.Fatal("server và client dẫn xuất ra hai khoá khác nhau")
	}
}

// TestSharedKeyBindsSid: cùng một cặp khoá client, khác sid → khác khoá. Nếu
// không có ràng buộc này thì client dùng lại một cặp khoá cho nhiều phiên sẽ
// khiến mọi phiên chia sẻ một khoá AES.
func TestSharedKeyBindsSid(t *testing.T) {
	client, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("sinh khoá client: %v", err)
	}
	b64 := base64.StdEncoding.EncodeToString(client.PublicKey().Bytes())

	first, err := NewE2EEKeyPair()
	if err != nil {
		t.Fatalf("NewE2EEKeyPair: %v", err)
	}
	second, err := NewE2EEKeyPair()
	if err != nil {
		t.Fatalf("NewE2EEKeyPair: %v", err)
	}
	// Cùng sid nhưng hai lượt handshake khác nhau cũng phải ra khoá khác
	// (khoá server là ephemeral), và sid khác chắc chắn khác.
	k1, err := first.SharedKey(b64, testSid)
	if err != nil {
		t.Fatalf("shared 1: %v", err)
	}
	k2, err := second.SharedKey(b64, testSid)
	if err != nil {
		t.Fatalf("shared 2: %v", err)
	}
	if bytes.Equal(k1, k2) {
		t.Error("hai handshake phải sinh khoá server ephemeral khác nhau")
	}
	k3, err := first.SharedKey(b64, "kh-c-sid-0102030405060708090a0b0c0d0e0f")
	if err != nil {
		t.Fatalf("shared sid khác: %v", err)
	}
	if bytes.Equal(k1, k3) {
		t.Error("sid phải tham gia dẫn xuất khoá")
	}
}

func TestE2EESealOpenRoundTrip(t *testing.T) {
	serverKey, clientKey := handshakeFixture(t)
	aad := AAD(testSid, DirRequest)

	nonce, ct, err := Seal(serverKey, aad, []byte(`{"message":"xin chào"}`))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if strings.Contains(ct, "xin chào") || strings.Contains(nonce, "xin") {
		t.Fatal("cipher còn chứa plaintext")
	}
	// Client mở bằng khoá nó tự dẫn xuất — chiều đi ngược cũng phải khớp.
	plain, err := Open(clientKey, aad, nonce, ct)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := string(plain); got != `{"message":"xin chào"}` {
		t.Fatalf("plain = %q", got)
	}
}

func TestSealUsesFreshNonce(t *testing.T) {
	key, _ := handshakeFixture(t)
	aad := AAD(testSid, DirRequest)
	nonce1, ct1, err := Seal(key, aad, []byte("như nhau"))
	if err != nil {
		t.Fatalf("seal 1: %v", err)
	}
	nonce2, ct2, err := Seal(key, aad, []byte("như nhau"))
	if err != nil {
		t.Fatalf("seal 2: %v", err)
	}
	if nonce1 == nonce2 || ct1 == ct2 {
		t.Fatal("hai lần seal cùng nội dung phải ra nonce và cipher khác nhau")
	}
}

// TestOpenRejectsTampering: sửa 1 byte cipher, đổi aad (sid/hướng), hoặc đưa
// khoá khác — tất cả phải lỗi, không được giải mã ra nội dung cũ.
func TestOpenRejectsTampering(t *testing.T) {
	key, _ := handshakeFixture(t)
	aad := AAD(testSid, DirRequest)
	nonce, ct, err := Seal(key, aad, []byte("payload"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	if _, err := Open(key, aad, nonce, flipBase64(ct)); !errors.Is(err, ErrEnvelopeBad) {
		t.Errorf("cipher bị sửa: err = %v", err)
	}
	if _, err := Open(key, AAD(testSid, DirResponse), nonce, ct); !errors.Is(err, ErrEnvelopeBad) {
		t.Errorf("phong bì request mở bằng AAD response: err = %v", err)
	}
	if _, err := Open(key, AAD("khac-sid", DirRequest), nonce, ct); !errors.Is(err, ErrEnvelopeBad) {
		t.Errorf("phong bì sid này mở bằng sid khác: err = %v", err)
	}
	otherKey, _ := handshakeFixture(t)
	if _, err := Open(otherKey, aad, nonce, ct); !errors.Is(err, ErrEnvelopeBad) {
		t.Errorf("mở bằng khoá phiên khác: err = %v", err)
	}
	if _, err := Open(key, aad, nonce, "không-phải-base64!!"); !errors.Is(err, ErrEnvelopeBad) {
		t.Errorf("nonce/cipher hỏng base64: err = %v", err)
	}
}

func TestSealRejectsWrongKeySize(t *testing.T) {
	if _, _, err := Seal(make([]byte, 16), nil, []byte("x")); err == nil {
		t.Fatal("khoá không đủ 32 byte phải bị từ chối")
	}
	if _, err := Open(make([]byte, 33), nil, "AAA=", "AAA="); err == nil {
		t.Fatal("Open cũng phải kiểm tra độ dài khoá")
	}
}

// TestSharedKeyRejectsBadPeer: handshake nhận chuỗi tuỳ ý từ client — phải báo
// ErrPeerKey để API dịch thành 400, không panic, không 500.
func TestSharedKeyRejectsBadPeer(t *testing.T) {
	server, err := NewE2EEKeyPair()
	if err != nil {
		t.Fatalf("NewE2EEKeyPair: %v", err)
	}
	cases := map[string]string{
		"không phải base64": "!!!!",
		"sai độ dài":        base64.StdEncoding.EncodeToString([]byte("ngắn")),
	}
	for name, value := range cases {
		if _, err := server.SharedKey(value, testSid); !errors.Is(err, ErrPeerKey) {
			t.Errorf("%s: err = %v, muốn ErrPeerKey", name, err)
		}
	}
}

func TestNewSessionIDUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		sid, err := NewSessionID()
		if err != nil {
			t.Fatalf("NewSessionID: %v", err)
		}
		if len(sid) != sessionIDBytes*2 {
			t.Fatalf("độ dài sid = %d, muốn %d", len(sid), sessionIDBytes*2)
		}
		if strings.ContainsAny(sid, "+/=") {
			t.Fatalf("sid phải hex (an toàn trong header): %q", sid)
		}
		if seen[sid] {
			t.Fatalf("sid trùng nhau: %q", sid)
		}
		seen[sid] = true
	}
}

// TestDecodeB64AcceptsBothEncodings: FE gửi chuẩn có padding, script test có
// thể gửi bản rút gọn — server không được từ chối chỉ vì thiếu "==".
func TestDecodeB64AcceptsBothEncodings(t *testing.T) {
	raw := []byte("payload của chat")
	padded := base64.StdEncoding.EncodeToString(raw)
	unpadded := base64.RawStdEncoding.EncodeToString(raw)
	for _, s := range []string{padded, unpadded} {
		got, err := decodeB64(s)
		if err != nil || !bytes.Equal(got, raw) {
			t.Fatalf("decodeB64(%q) = %q, err = %v", s, got, err)
		}
	}
	if _, err := decodeB64("c#h#uỗi rác"); err == nil {
		t.Fatal("chuỗi không phải base64 phải lỗi")
	}
}

// flipBase64 đổi bit cuối của byte đầu tiên trong bản mã (giữ nguyên độ dài).
func flipBase64(s string) string {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic(err)
	}
	raw[0] ^= 0x01
	return base64.StdEncoding.EncodeToString(raw)
}
