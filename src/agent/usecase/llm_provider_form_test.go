package usecase

import (
	"net/http"
	"testing"

	apperrors "knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"
)

// TestValidateProviderConnectionShared khoá chốt luật MỘT cho cả hai đường:
// provider chung (admin) và kết nối riêng của user (BYOK) phải chặn giống
// nhau, khác nhau duy nhất ở câu chữ và ở việc kết nối riêng bắt buộc có nhãn.
// Trước khi gom, mỗi đường có một bản riêng nên dễ lệch nhau.
func TestValidateProviderConnectionShared(t *testing.T) {
	cases := []struct {
		name    string
		kind    string
		baseURL string
		hasKey  bool
		wantErr bool
	}{
		{"thiếu type", "", "https://x/v1", true, true},
		{"type lạ", "anthropic", "https://x/v1", true, true},
		{"thiếu base_url", config.LLMTypeOpenAI, "", true, true},
		{"base_url không phải http", config.LLMTypeOpenAI, "ftp://x/v1", true, true},
		{"openai thiếu key", config.LLMTypeOpenAI, "https://x/v1", false, true},
		{"ollama không key vẫn được", config.LLMTypeOllama, "http://localhost:11434", false, false},
		{"openai đủ", config.LLMTypeOpenAI, "https://x/v1", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shared := validateProviderConnection("provider", "p", tc.kind, tc.baseURL, tc.hasKey)
			admin := validateConnection("p", config.LLMProviderConfig{
				Type: tc.kind, BaseURL: tc.baseURL, APIKey: keyOrEmpty(tc.hasKey),
			})
			user := validateUserProvider(&domain.LLMUserProvider{
				Label: "Kết nối của tôi", Type: tc.kind, BaseURL: tc.baseURL,
				APIKeyCipher: cipherOrEmpty(tc.hasKey),
			})
			if tc.wantErr {
				if shared == nil || admin == nil || user == nil {
					t.Fatalf("cả 3 đường phải chặn: shared=%v admin=%v user=%v", shared, admin, user)
				}
				for name, err := range map[string]error{"shared": shared, "admin": admin, "user": user} {
					if code := apperrors.From(err).HttpCode; code != http.StatusBadRequest {
						t.Errorf("%s: http code = %d, muốn 400", name, code)
					}
				}
				return
			}
			if shared != nil || admin != nil || user != nil {
				t.Fatalf("cả 3 đường phải cho qua: shared=%v admin=%v user=%v", shared, admin, user)
			}
		})
	}
}

// TestUserProviderNeedsLabel là điều RIÊNG của đường user: kết nối riêng bắt
// buộc có nhãn (provider chung fallback về slug còn dùng được).
func TestUserProviderNeedsLabel(t *testing.T) {
	row := &domain.LLMUserProvider{Type: config.LLMTypeOpenAI, BaseURL: "https://x/v1", APIKeyCipher: "c"}
	err := validateUserProvider(row)
	if err == nil {
		t.Fatal("kết nối riêng không có nhãn phải bị chặn")
	}
	if code := apperrors.From(err).HttpCode; code != http.StatusBadRequest {
		t.Errorf("http code = %d, muốn 400", code)
	}
}

// TestKeySourceOf khoá luật nguồn key chung: DB thắng config, không có thì rỗng.
func TestKeySourceOf(t *testing.T) {
	if got := keySourceOf(true, true); got != keySourceDB {
		t.Errorf("key trong DB phải thắng, nhận %q", got)
	}
	if got := keySourceOf(false, true); got != keySourceConfig {
		t.Errorf("chỉ có key trong config → %q", got)
	}
	if got := keySourceOf(false, false); got != "" {
		t.Errorf("không có key → %q, muốn rỗng", got)
	}
}

// keyOrEmpty / cipherOrEmpty dựng giá trị giả cho key mà không cần mã hoá thật —
// chỉ kiểm tra luật chặn, không kiểm tra giải mã.
func keyOrEmpty(has bool) string {
	if has {
		return "k"
	}
	return ""
}

func cipherOrEmpty(has bool) string {
	if has {
		return "cipher"
	}
	return ""
}
