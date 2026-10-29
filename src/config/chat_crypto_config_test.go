package config

import (
	"path/filepath"
	"testing"
)

// TestChatCryptoDefaults: config không khai báo chat_crypto vẫn chạy được —
// mặc định "optional" là cửa rollout (FE chưa nâng cấp vẫn gửi plaintext được),
// không phải "off" (mất luôn tính năng) hay "required" (đá bay FE cũ).
func TestChatCryptoDefaults(t *testing.T) {
	cfg := &appConfig{}
	if err := applyChatCryptoDefaults(cfg); err != nil {
		t.Fatalf("applyChatCryptoDefaults: %v", err)
	}
	if cfg.ChatCrypto.Mode != ChatCryptoOptional {
		t.Errorf("mode = %q, want %q", cfg.ChatCrypto.Mode, ChatCryptoOptional)
	}
	if cfg.ChatCrypto.SessionTTLMinutes != 720 {
		t.Errorf("ttl = %d, want 720", cfg.ChatCrypto.SessionTTLMinutes)
	}
}

// TestChatCryptoRejectsUnknownMode: giá trị lạ phải fail lúc boot. Im lặng coi
// là "off" còn tệ hơn: admin tưởng đã bật mã hoá mà không có gì được mã hoá.
func TestChatCryptoRejectsUnknownMode(t *testing.T) {
	for _, mode := range []string{"enable", "true", "E2EE", "require"} {
		cfg := &appConfig{}
		cfg.ChatCrypto.Mode = mode
		if err := applyChatCryptoDefaults(cfg); err == nil {
			t.Errorf("mode %q phải bị từ chối", mode)
		}
	}
}

// TestChatCryptoNormalizesAndClamps: mode viết hoa/có khoảng trắng vẫn nhận
// (admin gõ tay trong JSON), TTL âm về mặc định, TTL quá 7 ngày bị kẹp — phiên
// không được sống vĩnh viễn trong Redis.
func TestChatCryptoNormalizesAndClamps(t *testing.T) {
	cfg := &appConfig{}
	cfg.ChatCrypto.Mode = "  REQUIRED "
	cfg.ChatCrypto.SessionTTLMinutes = -5
	if err := applyChatCryptoDefaults(cfg); err != nil {
		t.Fatalf("applyChatCryptoDefaults: %v", err)
	}
	if cfg.ChatCrypto.Mode != ChatCryptoRequired {
		t.Errorf("mode = %q, want %q", cfg.ChatCrypto.Mode, ChatCryptoRequired)
	}
	if cfg.ChatCrypto.SessionTTLMinutes != 720 {
		t.Errorf("ttl âm = %d, want mặc định 720", cfg.ChatCrypto.SessionTTLMinutes)
	}

	cfg = &appConfig{}
	cfg.ChatCrypto.SessionTTLMinutes = 99999
	if err := applyChatCryptoDefaults(cfg); err != nil {
		t.Fatalf("applyChatCryptoDefaults: %v", err)
	}
	if cfg.ChatCrypto.SessionTTLMinutes != 7*24*60 {
		t.Errorf("ttl phải bị kẹp về 7 ngày, got %d", cfg.ChatCrypto.SessionTTLMinutes)
	}
}

// TestProjectConfigHasChatCrypto khóa section đã ship trong configs/config.json:
// thiếu hoặc sai mode ở đó là Load fail ngay lúc boot, nên test này là chỗ
// phát hiện sớm trước khi deploy.
func TestProjectConfigHasChatCrypto(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "configs", "config.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cc := cfg.GetChatCrypto()
	if cc.Mode != ChatCryptoOptional && cc.Mode != ChatCryptoOff && cc.Mode != ChatCryptoRequired {
		t.Errorf("chat_crypto.mode = %q, muốn off/optional/required", cc.Mode)
	}
	if cc.SessionTTLMinutes <= 0 {
		t.Errorf("chat_crypto.session_ttl_minutes = %d, want > 0", cc.SessionTTLMinutes)
	}
}
