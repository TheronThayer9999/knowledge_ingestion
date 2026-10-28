package utils

import (
	"strings"
	"testing"
)

// TestSealOpenRoundTrip: cipher phải giải mã đúng với cùng secret, và mỗi lần
// Seal ra chuỗi khác nhau (nonce ngẫu nhiên) — nếu không, hai provider cùng key
// sẽ trùng cipher trong DB, lộ "hai bên xài chung một key".
func TestSealOpenRoundTrip(t *testing.T) {
	first, err := SealString("sk-secret", "jwt-secret")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	second, err := SealString("sk-secret", "jwt-secret")
	if err != nil {
		t.Fatalf("seal lần 2: %v", err)
	}
	if first == second {
		t.Error("hai lần seal phải ra cipher khác nhau (nonce ngẫu nhiên)")
	}
	if strings.Contains(first, "sk-secret") {
		t.Fatalf("cipher chứa plaintext: %q", first)
	}
	got, err := OpenString(first, "jwt-secret")
	if err != nil || got != "sk-secret" {
		t.Fatalf("open = %q, err = %v", got, err)
	}
}

// TestOpenStringWrongSecret: đổi secret (xoay JWT_SECRET) thì phải lỗi để
// caller fallback về key config — im lặng trả chuỗi rác còn tệ hơn.
func TestOpenStringWrongSecret(t *testing.T) {
	cipher, err := SealString("sk-secret", "jwt-secret")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := OpenString(cipher, "secret-khác"); err == nil {
		t.Fatal("secret khác phải giải mã lỗi")
	}
	if _, err := OpenString("không-phải-base64!!", "jwt-secret"); err == nil {
		t.Fatal("chuỗi không phải base64 phải lỗi")
	}
	if _, err := SealString("x", ""); err == nil {
		t.Fatal("thiếu secret phải lỗi, không lưu cipher không giải mã được")
	}
}
