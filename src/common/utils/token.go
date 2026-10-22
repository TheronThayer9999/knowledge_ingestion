package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// OpaqueTokenBytes là entropy của refresh token: 256 bit. Đủ để không thể
// đoán/brute-force kể cả khi endpoint refresh không có rate limit dày.
const OpaqueTokenBytes = 32

// NewOpaqueToken sinh refresh token dạng opaque (không mang dữ liệu, không
// ký) và hash SHA-256 của nó. Trả CẢ HAI: plain chỉ đưa về client đúng 1 lần
// lúc login/refresh, hash mới là thứ lưu DB — DB lộ cũng không dùng lại được.
func NewOpaqueToken() (plain, hash string, err error) {
	buf := make([]byte, OpaqueTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("sinh refresh token: %w", err)
	}
	plain = base64.RawURLEncoding.EncodeToString(buf)
	return plain, HashToken(plain), nil
}

// HashToken là hàm thuần: cùng token luôn ra cùng hash, nên tra DB theo hash
// được (unique index) mà không cần lưu token gốc.
func HashToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}
