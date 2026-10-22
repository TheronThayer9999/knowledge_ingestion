package utils

import (
	"testing"
)

func TestNewOpaqueTokenSinhTokenNgauNhienVaHashDung(t *testing.T) {
	plain, hash, err := NewOpaqueToken()
	if err != nil {
		t.Fatalf("NewOpaqueToken: %v", err)
	}
	if plain == "" || hash == "" {
		t.Fatal("token/hash rỗng")
	}
	if plain == hash {
		t.Fatal("phải trả token gốc và hash khác nhau")
	}
	if len(hash) != 64 {
		t.Fatalf("hash SHA-256 phải dài 64 hex, được %d", len(hash))
	}
	if got := HashToken(plain); got != hash {
		t.Fatalf("HashToken không ổn định: %s != %s", got, hash)
	}

	other, otherHash, err := NewOpaqueToken()
	if err != nil {
		t.Fatalf("NewOpaqueToken lần 2: %v", err)
	}
	if other == plain || otherHash == hash {
		t.Fatal("2 lần sinh phải khác nhau — entropy hỏng")
	}
}
