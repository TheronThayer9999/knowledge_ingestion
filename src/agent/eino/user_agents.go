package eino

import (
	"fmt"
	"strings"
	"sync"

	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"

	"knowledge_ingestion/src/config"
)

// userAgentsCapacity là số agent provider RIÊNG của user được giữ trong bộ
// nhớ đệm. Mỗi entry giữ 1 HTTP client + rate limiter của provider, nên để
// vô hạn thì user tạo nhiều kết nối sẽ làm phình bộ nhớ app.
const userAgentsCapacity = 64

// UserAgents là registry agent cho provider RIÊNG của từng user (BYOK).
//
// Khác ProviderAgents — map slug → agent dùng chung toàn hệ thống — ở đây
// khoá là (userID, providerID) vì mỗi user có key khác nhau cho cùng một
// base_url. Dùng chung key là lộ key của user này cho user kia, nên tách
// hoàn toàn: entry của user A không bao giờ nằm trong bản đồ của user B.
//
// Không có "provider mặc định" ở đây: lượt chat chỉ dùng provider riêng khi
// client gửi slug dạng u_<id>, còn lại rơi về registry chung.
type UserAgents struct {
	mu sync.Mutex
	// entries khoá cache theo userKey (userID:providerID).
	entries map[string]*userAgentEntry
	// order là thứ tự LRU — entry dùng gần nhất nằm cuối.
	order []string
	// system/tools giữ lại để dựng agent MỘT kết nối mà không phải dựng lại
	// cả registry.
	system InstructionSet
	tools  []einotool.BaseTool
	// capacity là trần số entry trong bộ nhớ đệm.
	capacity int
}

// userAgentEntry là một bộ đã dựng cho MỘT kết nối của MỘT user, kèm
// fingerprint cấu hình để phát hiện base_url/key đổi thì phải dựng lại.
type userAgentEntry struct {
	entry       *providerEntry
	fingerprint string
}

// NewUserAgents dựng registry rỗng — wiring ở loader gọi, giống
// NewProviderAgents nhưng không cần danh sách provider vì provider riêng
// sinh ra theo yêu cầu của từng user.
func NewUserAgents(system InstructionSet, tools []einotool.BaseTool) *UserAgents {
	return &UserAgents{
		entries:  make(map[string]*userAgentEntry),
		system:   system,
		tools:    tools,
		capacity: userAgentsCapacity,
	}
}

// Resolve trả agent + model cho một kết nối riêng của user. key là khoá
// cache ("userID:providerID"), pcfg là cấu hình ĐÃ hợp nhất (base_url + key
// đã giải mã), model rỗng thì lấy model mặc định của kết nối.
//
// Trả kèm client thô (einomodel.ToolCallingChatModel) để bước phân loại câu
// hỏi chạy trên ĐÚNG provider của lượt đó — user trả tiền cho mọi call
// của mình, không call nào lấy key của hệ thống.
func (u *UserAgents) Resolve(key string, pcfg config.LLMProviderConfig,
	model string, thinking bool) (*ChatAgent, string, einomodel.ToolCallingChatModel, error) {
	if u == nil {
		return nil, "", nil, fmt.Errorf("eino agent: chưa được dựng")
	}
	fp := providerFingerprint(pcfg)
	u.mu.Lock()
	defer u.mu.Unlock()
	if cached, ok := u.entries[key]; ok && cached.fingerprint == fp {
		u.touch(key)
		return pickUserAgent(cached.entry, model, thinking)
	}
	entry, err := newProviderEntry(key, NewConfigProvider(key, pcfg), u.system, u.tools)
	if err != nil {
		return nil, "", nil, err
	}
	u.entries[key] = &userAgentEntry{entry: entry, fingerprint: fp}
	u.touch(key)
	u.evict()
	return pickUserAgent(entry, model, thinking)
}

// Drop bỏ một kết nối khỏi bộ nhớ đệm — user xoá kết nối thì agent cũ không
// được nằm lại giữ key đã xoá.
func (u *UserAgents) Drop(key string) {
	if u == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.entries, key)
	u.dropOrder(key)
}

// pickUserAgent chọn agent (thường hoặc bản suy nghĩ) + model của một entry
// đã dựng. Model rỗng và kết nối không có model mặc định thì lỗi rõ ràng —
// user phải chọn model, đừng đoán.
func pickUserAgent(entry *providerEntry, model string,
	thinking bool) (*ChatAgent, string, einomodel.ToolCallingChatModel, error) {
	agent := entry.agent
	if thinking && entry.thinking != nil && entry.thinking != entry.agent {
		agent = entry.thinking
	}
	name := strings.TrimSpace(model)
	if name == "" {
		name = entry.defaultModel
	}
	if name == "" {
		return nil, "", nil, fmt.Errorf("eino agent: kết nối này chưa có model mặc định, hãy chọn model")
	}
	return agent, name, entry.model, nil
}

// providerFingerprint mô tả cấu hình quyết định client: đổi base_url, key
// hay model mặc định thì phải dựng lại agent (client nhúng key lúc dựng).
func providerFingerprint(pcfg config.LLMProviderConfig) string {
	return strings.Join([]string{pcfg.Type, pcfg.BaseURL, pcfg.APIKey, pcfg.Model}, "\x00")
}

// touch đưa key lên cuối order — vị trí cuối = dùng gần nhất.
func (u *UserAgents) touch(key string) {
	u.dropOrder(key)
	u.order = append(u.order, key)
}

// dropOrder gỡ key khỏi order (nếu có).
func (u *UserAgents) dropOrder(key string) {
	for i, item := range u.order {
		if item == key {
			u.order = append(u.order[:i], u.order[i+1:]...)
			return
		}
	}
}

// evict bỏ entry cũ nhất khi vượt trần — bộ nhớ đệm không phải kho.
func (u *UserAgents) evict() {
	for len(u.order) > u.capacity {
		oldest := u.order[0]
		u.order = u.order[1:]
		delete(u.entries, oldest)
	}
}
