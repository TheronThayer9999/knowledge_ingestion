package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

// Primitive mã hoá payload theo phiên (E2EE giữa trình duyệt và server) —
// Đợt 29. Mô hình đã chốt: trình duyệt sinh một cặp khoá X25519 cho mỗi phiên,
// gửi public key lên /crypto/handshake, server trả public key của nó, hai bên
// tính cùng một shared secret rồi dẫn xuất khoá AES-256-GCM bằng HKDF-SHA256.
//
// Server GIỮ khoá phiên nên giải mã được: RAG, phân loại câu hỏi và trích dẫn
// vẫn chạy bình thường. Thứ mà cách này chống được là nghe lén trên đường truyền
// (proxy, wifi công cộng, log của các trung gian), không phải server độc hại.
//
// Hướng (direction) nằm trong AAD nên một phong bì của request không thể bị
// coi là response và ngược lại; sid nằm trong AAD nên cipher của phiên này
// không chuyển được sang phiên khác.

// Hằng số khuôn dạng — đổi là làm hỏng mọi phiên đang mở, chỉ sửa khi
// đồng thời bump `v` của phong bì và cho FE biết.
const (
	// E2EEInfo là HKDF info, tách khoá này khỏi mọi ngữ cảnh HKDF khác.
	E2EEInfo = "ki-api-aes-256-gcm"
	// E2EESaltPrefix đứng trước sid trong HKDF salt: hai phiên khác sid thì
	// khoá khác nhau, dù client có dùng lại một cặp khoá.
	E2EESaltPrefix = "ki-e2ee-v1|"
	// DirRequest / DirResponse là direction gắn vào AAD của phong bì.
	DirRequest  = "req"
	DirResponse = "rsp"

	sessionIDBytes = 16
)

// ErrPeerKey báo public key của phía bên kia không hợp lệ (sai độ dài, điểm
// bị từ chối). Caller dịch thành 400, không phải 500.
var ErrPeerKey = errors.New("khoá public của client không hợp lệ")

// ErrEnvelopeBad là phong bì không mở được: base64 hỏng, độ dài sai, hoặc
// AES-GCM tag không hợp lệ.
var ErrEnvelopeBad = errors.New("phong bì mã hoá không hợp lệ")

// E2EEKeyPair là một cặp khoá X25519 dùng cho đúng một handshake.
//
// State nằm trong struct (không có biến package): private key không bao giờ
// rời khỏi tiến trình, và mỗi phiên có một instance riêng.
type E2EEKeyPair struct {
	private *ecdh.PrivateKey
	public  *ecdh.PublicKey
}

// NewE2EEKeyPair sinh một cặp khoá X25519 tạm thời.
func NewE2EEKeyPair() (*E2EEKeyPair, error) {
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("sinh khoá X25519: %w", err)
	}
	return &E2EEKeyPair{private: private, public: private.PublicKey()}, nil
}

// PublicKeyB64 trả public key 32 byte ở dạng base64 — thứ duy nhất được gửi đi.
func (k *E2EEKeyPair) PublicKeyB64() string {
	return encodeB64(k.public.Bytes())
}

// SharedKey hoàn tất ECDH với public key của client rồi dẫn xuất khoá phiên
// 32 byte. Trả về khoá chung cho cả hai phía; thông số HKDF phải giống hệt
// phía client, sai một ký tự là hai bên có hai khoá khác nhau và mọi phong bì
// Open đều fail.
func (k *E2EEKeyPair) SharedKey(clientPublicB64, sid string) ([]byte, error) {
	raw, err := decodeB64(clientPublicB64)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPeerKey, err)
	}
	peer, err := ecdh.X25519().NewPublicKey(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPeerKey, err)
	}
	secret, err := k.private.ECDH(peer)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPeerKey, err)
	}
	return DeriveKey(secret, sid)
}

// DeriveKey tách khỏi ECDH để test phía client (WebCrypto) so sánh được từng
// bước: input là shared secret thô, output là khoá AES-256.
func DeriveKey(secret []byte, sid string) ([]byte, error) {
	reader := hkdf.New(sha256.New, secret,
		[]byte(E2EESaltPrefix+sid), []byte(E2EEInfo))
	key := make([]byte, 32)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, fmt.Errorf("dẫn xuất khoá phiên: %w", err)
	}
	return key, nil
}

// NewSessionID sinh id phiên đủ dài và URL-safe để nhét vào header.
func NewSessionID() (string, error) {
	raw := make([]byte, sessionIDBytes)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", fmt.Errorf("sinh session id: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// AAD khoá phong bì vào đúng (phiên, hướng). Đây là lý do sid phải là một
// tham số bắt buộc thay vì để caller tự ghép chuỗi.
func AAD(sid, direction string) []byte {
	return []byte("ki-e2ee-v1|" + sid + "|" + direction)
}

// Seal mã hoá plain với khoá phiên, nonce 12 byte ngẫu nhiên cho mỗi lần gọi.
// Trả hai phần base64 rời nhau đúng như khuôn phong bì trên đường dây.
//
// Nonce ngẫu nhiên là đánh đổi có ý thức: nó đơn giản hơn bộ đếm và không bắt
// client phải giữ state giữa các lượt, an toàn trong phạm vi khối lượng chat.
func Seal(key []byte, aad, plain []byte) (nonceB64, ctB64 string, err error) {
	gcm, err := newSessionGCM(key)
	if err != nil {
		return "", "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", "", fmt.Errorf("sinh nonce: %w", err)
	}
	sealed := gcm.Seal(nil, nonce, plain, aad)
	return encodeB64(nonce), encodeB64(sealed), nil
}

// Open giải mã một phong bì. Mọi lỗi ở đây gộp thành một sentinel: caller
// không được phân biệt "sai khoá" và "toàn vẹn hỏng" ra ngoài response, vì cả
// hai đều chỉ có nghĩa "bên gửi không có khoá phiên đúng".
func Open(key []byte, aad []byte, nonceB64, ctB64 string) ([]byte, error) {
	gcm, err := newSessionGCM(key)
	if err != nil {
		return nil, err
	}
	nonce, err := decodeB64(nonceB64)
	if err != nil {
		return nil, ErrEnvelopeBad
	}
	ct, err := decodeB64(ctB64)
	if err != nil {
		return nil, ErrEnvelopeBad
	}
	if len(nonce) != gcm.NonceSize() || len(ct) < gcm.Overhead() {
		return nil, ErrEnvelopeBad
	}
	plain, err := gcm.Open(nil, nonce, ct, aad)
	if err != nil {
		return nil, ErrEnvelopeBad
	}
	return plain, nil
}

func newSessionGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("khoá phiên phải 32 byte, nhận %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("tạo AES: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("tạo GCM: %w", err)
	}
	return gcm, nil
}

// encodeB64 dùng StdEncoding vì WebCrypto chỉ xuất ArrayBuffer rồi btoa: btoa
// luôn thêm padding, nên phía dây luôn là chuẩn có padding.
func encodeB64(raw []byte) string {
	return base64.StdEncoding.EncodeToString(raw)
}

// decodeB64 chấp nhận cả bản có và không có padding: server không nên từ chối
// chỉ vì một phía nào đó cắt "=" đi.
func decodeB64(s string) ([]byte, error) {
	if raw, err := base64.StdEncoding.DecodeString(s); err == nil {
		return raw, nil
	}
	raw, err := base64.RawStdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("base64 hỏng: %w", err)
	}
	return raw, nil
}
