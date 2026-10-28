package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Khoá mã hoá API key admin nhập từ UI (Đợt 28 nấc 2) — AES-256-GCM, nonce
// ngẫu nhiên mỗi lần mã hoá, đầu ra base64 (nonce||ciphertext).
//
// Khoá dẫn xuất SHA-256 từ secret của JWT: app đã fail-fast khi thiếu secret
// nên không đẻ thêm một config bắt buộc nữa, và secret đó cũng là thứ duy nhất
// đã được coi là bí mật triển khai. Hệ quả PHẢI biết: đổi JWT_SECRET thì các
// key đã lưu không giải mã được nữa — chúng bị bỏ qua (fallback key config) và
// admin nhập lại; không có đường tự phục hồi, đó là đánh đổi của việc không
// thêm KMS.

// ErrNoSecret báo chưa có secret để dẫn xuất khoá — caller phải fail trước khi
// ghi DB, không lưu cipher không giải mã được.
var ErrNoSecret = errors.New("thiếu secret để mã hoá api key")

// SealString mã hoá plaintext thành chuỗi base64 an toàn lưu DB.
func SealString(plain, secret string) (string, error) {
	gcm, err := newGCM(secret)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("sinh nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

// OpenString giải mã chuỗi do SealString tạo ra. Lỗi ở đây là "key lưu DB hỏng
// hoặc secret đã đổi" — caller chỉ được warn rồi fallback, không fail request.
func OpenString(encoded, secret string) (string, error) {
	gcm, err := newGCM(secret)
	if err != nil {
		return "", err
	}
	raw, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("api key đã lưu không phải base64: %w", err)
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("api key đã lưu quá ngắn")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("giải mã api key: %w", err)
	}
	return string(plain), nil
}

func newGCM(secret string) (cipher.AEAD, error) {
	if secret == "" {
		return nil, ErrNoSecret
	}
	key := sha256.Sum256([]byte("knowledge_ingestion:llm-provider-key:" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
