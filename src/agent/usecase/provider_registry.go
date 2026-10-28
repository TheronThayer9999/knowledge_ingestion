package usecase

import (
	einomodel "github.com/cloudwego/eino/components/model"

	"knowledge_ingestion/src/agent/eino"
	"knowledge_ingestion/src/config"
)

// Các interface ở file này là "cái mà usecase cần", không phải "toàn bộ năng lực
// của package triển khai". Đúng khuôn DIP của repo: service nhận interface, chỉ
// loader biết triển khai cụ thể — nhờ vậy test dựng được fake registry mà không
// phải kéo theo client LLM thật, và đổi backend agent không phải sửa usecase.

// IProviderRegistry là phần ĐỌC của registry agent chat theo backend LLM.
type IProviderRegistry interface {
	// EffectiveProvider chuẩn hoá slug request gửi lên; rỗng thì provider mặc
	// định — chat ghi response đúng provider THỰC ĐÃ CHẠY.
	EffectiveProvider(provider string) string
	// Resolve chọn agent + model cho 1 lượt; provider chưa dựng được thì lỗi để
	// caller map 400 (không bao giờ 500 vì một cái key thiếu).
	Resolve(provider, model string, thinking bool) (*eino.ChatAgent, string, error)
	// DefaultProvider là slug dùng khi request không ép provider.
	DefaultProvider() string
	// RawModel trả client thô của provider — classifier vòng 1 dùng chung.
	RawModel(slug string) (einomodel.ToolCallingChatModel, bool)
	// Has: provider này đã dựng được agent (UI báo "sẵn sàng").
	Has(slug string) bool
	// ThinkingDistinct: provider có client "suy nghĩ" RIÊNG hay không — cờ suy
	// luận sâu chỉ bật được ở provider trả lời true.
	ThinkingDistinct(slug string) bool
	// AvailableProviders liệt kê slug đang chạy được cho thông báo lỗi.
	AvailableProviders() string
}

// IProviderRegistryAdmin là IProviderRegistry + phần GHI mà popup Nhà cung cấp
// AI cần (Đợt 28 nấc 2 + 3). Tách riêng để service chỉ-đọc (llmOptionsService)
// không cầm được quyền gỡ agent của người đang chat.
type IProviderRegistryAdmin interface {
	IProviderRegistry
	// Rebuild dựng lại client + agent của MỘT provider theo cấu hình đã hợp nhất.
	Rebuild(slug string, pcfg config.LLMProviderConfig) error
	// Remove tháo agent của provider (admin tắt hoặc xoá).
	Remove(slug string)
	// SetDefaultModel đổi model mặc định của provider cho lượt sau.
	SetDefaultModel(slug, model string)
}
