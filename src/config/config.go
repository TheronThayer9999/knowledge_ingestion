package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// IConfig định nghĩa contract cho việc lấy cấu hình
type IConfig interface {
	GetApp() AppConfig
	GetDatabase() DBConfig
	GetStorage() S3Config
	GetEmbedding() EmbeddingConfig
	GetLLMs() LLMsConfig
	GetQdrant() QdrantConfig
	GetOCR() OCRConfig
	GetRedis() RedisConfig // Thêm method này
	GetRabbitMQ() RabbitMQConfig
	GetJWT() JWTConfig
	GetCORS() CorsConfig
	GetWorker() WorkerConfig
	GetAgent() AgentConfig
	GetVLM() VLMConfig
}

// Implementations
type appConfig struct {
	App       AppConfig       `json:"app"`
	Database  DBConfig        `json:"database"`
	Storage   S3Config        `json:"storage"`
	Embedding EmbeddingConfig `json:"embedding"`
	LLMs      LLMsConfig      `json:"llms"`
	Qdrant    QdrantConfig    `json:"qdrant"`
	OCR       OCRConfig       `json:"ocr"`
	RedisCli  RedisConfig     `json:"redis"`
	Rabbit    RabbitMQConfig  `json:"rabbitmq"`
	JWTCli    JWTConfig       `json:"jwt"`
	Cors      CorsConfig      `json:"cors"`
	Worker    WorkerConfig    `json:"worker"`
	Agent     AgentConfig     `json:"agent"`
	VLM       VLMConfig       `json:"vlm"`
}

type AppConfig struct {
	Name string `json:"name"`
	Port int    `json:"port"`
	Env  string `json:"env"`
}

type DBConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	DBName   string `json:"dbname"`
	SSLMode  string `json:"sslmode"`
}

type S3Config struct {
	Type      string `json:"type"`
	Endpoint  string `json:"s3_endpoint"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Bucket    string `json:"bucket_name"`
}

type EmbeddingConfig struct {
	Provider string `json:"provider"` // "ollama", "openai", ...
	Model    string `json:"model"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"` // Ollama để trống
	Dim      int    `json:"dim"`
}

// LLMsConfig gom toàn bộ backend LLM vào một map thống nhất theo tên slug —
// thêm backend mới (Groq, DeepSeek, Together, ...) chỉ cần khai báo thêm 1
// entry trong config, không sửa code. Request chọn backend qua field
// `provider`, model mặc định theo từng entry. Protocol lặp lại nhiều nhất
// (OpenAI-compatible: OpenRouter/Groq/OpenAI/DeepSeek) gói trong type
// "openai", Ollama native giữ type "ollama".
type LLMsConfig struct {
	// DefaultProvider là provider khi request không ép (field `provider`
	// rỗng) — trống thì mặc định "ollama".
	DefaultProvider string `json:"default_provider"`
	// Providers là map slug (request chọn qua field `provider`) → cấu hình.
	Providers map[string]LLMProviderConfig `json:"providers"`
}

// Protocol type của 1 entry LLMProviders — chọn cách dựng client.
const (
	// LLMTypeOllama: Ollama native /api/chat (local + cloud qua Bearer key).
	LLMTypeOllama = "ollama"
	// LLMTypeOpenAI: OpenAI-compatible /chat/completions — dùng chung cho
	// OpenRouter, Groq, OpenAI, DeepSeek, ... (cùng eino-ext openai).
	LLMTypeOpenAI = "openai"
)

// LLMProviderConfig là cấu hình chung 1 backend LLM. `type` bắt buộc phải
// là "ollama" hoặc "openai" (validate lúc Load, fail-fast). Với type
// "openai", api_key bắt buộc (rỗng thì loader bỏ qua provider — boot vẫn
// lên, request nào chọn nó mới 400); base_url bắt buộc vì không có default
// chung cho mọi backend OpenAI-compatible. Model rỗng nghĩa là chưa chọn —
// request phải ép model theo mỗi lượt.
type LLMProviderConfig struct {
	Type string `json:"type"`
	// Label là tên hiển thị trên UI; rỗng thì UI dùng chính slug. Không ảnh
	// hưởng cách chọn provider (request vẫn gửi slug).
	Label string `json:"label"`
	// Model là model mặc định của provider (rỗng = request phải ép model).
	Model string `json:"model"`
	// Models là danh sách model gợi ý cho menu chọn trên UI; rỗng thì lấy
	// đúng [model] — config cũ không có field này vẫn chạy nguyên. Đây chỉ là
	// metadata hiển thị: backend không chặn model ngoài danh sách (provider tự
	// từ chối model sai).
	Models      []string `json:"models"`
	BaseURL     string   `json:"base_url"`
	APIKey      string   `json:"api_key"`
	Temperature float64  `json:"temperature"`
	// TopP 0 = không gửi (provider tự quyết) — chỉ set khi muốn ép nucleus
	// sampling; Generally recommend alter TopP hoặc Temperature, không cả hai.
	TopP       float64 `json:"top_p"`
	MaxTokens  int     `json:"max_tokens"`
	TimeoutSec int     `json:"timeout_sec"`
	// RateLimitRPM giữ nhịp tối đa request/phút gửi vào backend (0 = không
	// giới hạn) — Groq gói free chỉ ~30 req/phút, vượt là 429. Token bucket
	// san đều theo thời gian thay vì dồn dập rồi đợi server chối.
	RateLimitRPM int `json:"rate_limit_rpm"`
}

// QdrantConfig giữ địa chỉ Qdrant — worker embed upsert vector qua gRPC
// (cổng 6334), REST (port 6333) chỉ dùng cho dashboard/MCP. Local không bật
// auth thì để token rỗng. Scheme tách riêng (http/https): https nghĩa là TLS
// đã bật ở server, client gRPC bật UseTLS theo.
type QdrantConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	GrpcPort int    `json:"grpc_port"`
	Token    string `json:"token"`
	Scheme   string `json:"scheme"`

	// Quantization nén vector để giảm RAM/đĩa khi số point lớn:
	// "" (không nén), "scalar" (int8, giảm 4 lần — mặc định khuyến nghị),
	// "binary" (giảm 32 lần, BẮT BUỘC bật rescore khi search).
	//
	// Collection mới nhận cấu hình này lúc tạo; collection đã tồn tại thì
	// worker bật thêm bằng UpdateCollection (best-effort, lỗi chỉ log).
	// Tắt nén trên collection đã nén phải làm tay — code không tự hạ cấu hình.
	Quantization string `json:"quantization"`
	// OnDiskVectors đặt vector gốc trên đĩa thay vì RAM. CHỈ áp lúc tạo
	// collection — Qdrant không đổi được sau đó, nên collection cũ sẽ chỉ
	// nhận cảnh báo trong log.
	OnDiskVectors bool `json:"on_disk_vectors"`
	// OnDiskPayload đặt payload trên đĩa. Payload ở đây chứa nguyên text
	// chunk (~1-2 KB/point) nên với kho lớn nó tốn RAM hơn cả vector.
	OnDiskPayload bool `json:"on_disk_payload"`
	// Rescore chấm lại top-k bằng vector gốc sau khi tìm bằng vector nén.
	// Bắt buộc với binary, khuyến nghị với scalar. Chỉ có tác dụng khi
	// collection thật sự đang nén.
	Rescore bool `json:"rescore"`
	// Oversampling lấy dư bao nhiêu lần số hit trước khi rescore (mặc định
	// 2.0) — cao hơn thì chính xác hơn, chậm hơn.
	Oversampling float64 `json:"oversampling"`
	// PrincipalIndex đặt payload index user_id thành "principal" (partition
	// dữ liệu theo tenant) để filter user vẫn đi HNSW thay vì quét rộng.
	// Chỉ áp được lúc index được tạo; index cũ giữ nguyên (xem log).
	PrincipalIndex bool `json:"principal_index"`
}

// OCRConfig giữ công tắc + đường dẫn engine Tesseract — worker chunk gọi qua
// os/exec cho file ảnh (jpg/png/webp), KHÔNG qua Python trung gian. Binary
// mặc định "tesseract" (ăn theo PATH, hợp Linux/prod); Windows dev trỏ thẳng
// "C:\Program Files\Tesseract-OCR\tesseract.exe" trong config.json hoặc env
// OCR_BINARY. Lang theo model tessdata, nhận chuỗi nhiều gói kiểu "vie+eng"
// (engine chọn model tốt hơn cho TỪNG từ — xem file _tesseract.md); tài liệu
// chỉ toàn tiếng Việt thì "vie" là đủ và nhanh hơn.
type OCRConfig struct {
	Enabled    bool   `json:"enabled"`
	Binary     string `json:"binary"`
	Lang       string `json:"lang"`
	TimeoutSec int    `json:"timeout_sec"`
	// RenderBinary là renderer PDF→ảnh cho trang scan (vd "pdftoppm") — rỗng
	// thì trang scan bỏ qua như trước. Cùng pattern os/exec như Binary nên
	// sau này tách OCR thành API riêng không phải sửa logic.
	RenderBinary string `json:"render_binary"`
	// RenderDPI độ phân giải render trang scan — 300 là điểm ngọt Tesseract.
	RenderDPI int `json:"render_dpi"`
	// MinTextChars ngưỡng "text layer mỏng" vẫn chạy OCR. MẶC ĐỊNH 0 (tắt):
	// đa số PDF không phải ảnh, OCR mỗi trang tốn ~2s nên bật rộng sẽ tốn
	// cả giờ máy cho vài trang có ích. Bật (vd 150) khi gặp file export từ
	// Word có text layer mỏng lẫn ảnh scan — dạng đó text không rỗng nên
	// chỉ-OCR-trang-rỗng sẽ bỏ sót phần ảnh.
	MinTextChars int `json:"min_text_chars"`
	// PSM là Page Segmentation Mode của Tesseract. 6 = giả định MỘT khối chữ
	// đồng nhất, hợp thân bản nhiều dòng; trang có bảng/cột thì nội dung bị
	// lẫn dòng. 3 (mặc định của Tesseract) tự dò bố cục nên chịu được bảng,
	// đổi lại chậm hơn. <= 0 thì dùng 6.
	PSM int `json:"psm"`
}

// redis cli
type RedisConfig struct {
	Host           string `json:"host"` // Đổi từ Addr sang Host để dễ quản lý
	Port           int    `json:"port"` // Thêm Port
	Password       string `json:"password"`
	DB             int    `json:"db"`
	SocketTimeout  int    `json:"socket_timeout"`
	RetryOnTimeout bool   `json:"retry_on_timeout"`
}

// RabbitMQConfig giữ kết nối broker + topology cho relay outbox (worker
// publish/consume event). Khớp docker/rabbit_mq/docker-compose.yml
// (user admin/admin123, vhost /, AMQP 5672, management UI 15672).
type RabbitMQConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Vhost    string `json:"vhost"`
	// Exchange worker tự declare lúc boot (direct, durable) — relay publish
	// event vào đây, queue bind theo event_type.
	Exchange string `json:"exchange"`
	// Queue hàng đợi chính của relay — worker tự declare + bind lúc boot.
	Queue string `json:"queue"`
}

// JWTConfig giữ bí mật ký token và thời hạn sống. Secret ưu tiên lấy từ env
// JWT_SECRET để production không commit secret vào file.
//
// ExpiryMinutes là hạn của ACCESS token. Muốn hạ xuống 15 phút thì FE phải
// gọi /auth/refresh trước — nếu hạ khi FE chưa hỗ trợ, user bị đá ra giữa
// phiên. Vì vậy mặc định vẫn 1440 (1 ngày) cho tới khi FE xong.
type JWTConfig struct {
	Secret        string `json:"secret"`
	ExpiryMinutes int    `json:"expiry_minutes"`
	// RefreshExpiryDays là hạn của MỘT refresh token (mỗi lần refresh lại
	// được cấp token mới với hạn mới — sliding).
	RefreshExpiryDays int `json:"refresh_expiry_days"`
	// RefreshAbsoluteDays là hạn tuyệt đối của cả phiên (family): dù refresh
	// liên tục, quá mốc này vẫn phải đăng nhập lại. Chặn trường hợp token bị
	// đánh cắp sống mãi.
	RefreshAbsoluteDays int `json:"refresh_absolute_days"`
}

// CorsConfig giữ danh sách origin được phép gọi API (Angular dev/prod).
type CorsConfig struct {
	AllowedOrigins []string `json:"allowed_origins"`
}

// WorkerConfig giữ nhịp janitor + chiến lược cắt chunk. TrimIntervalHours là
// nhịp quét dọn trang trung gian mồ côi (bài đã xoá hẳn mà job extract chết
// giữa lúc ghi). Đổi config + restart worker là ăn ngay, không cần build lại.
type WorkerConfig struct {
	TrimIntervalHours int `json:"trim_interval_hours"`
	// Chunker chọn chiến lược cắt chunk phase 1: "recursive" (Eino splitter
	// đệ quy, mặc định — rẻ, bulk), "semantic" (Eino splitter ngữ nghĩa qua
	// Ollama embed, đắt — tài liệu ưu tiên/văn bản hành chính nhiều viết
	// tắt), "manual" (Split thủ công cũ, giữ cho test/fallback).
	Chunker         string `json:"chunker"`
	EnhanceKeywords bool   `json:"enhance_keywords"`
	// LayoutDetector bật đo bố cục phức tạp (bảng, nhiều cột) cho PDF ngay
	// trong phase extract — thêm 1 lượt `pdftotext -layout` mỗi lần trích,
	// kết quả lưu vào article_pages.layout_score. Mặc định TẮT: bật là tăng
	// thời gian extract nên chỉ bật khi cần lọc ra trang cho L2/VLM.
	LayoutDetector bool `json:"layout_detector"`
}

// AgentConfig giữ đường dẫn thư mục prompt LLM của agent (prompts/*.md) —
// sửa prompt rồi restart app là ăn, không cần build lại.
type AgentConfig struct {
	PromptDir string `json:"prompt_dir"`
	// ChartEnabled bật tool chart_from_cite (Đợt 26). Con trỏ để "không ghi gì
	// trong config" = BẬT (default của tính năng), chỉ `false` tường minh mới tắt
	// — nút dừng cho một tính năng đụng cả 4 tầng mà không phải revert code.
	ChartEnabled *bool `json:"chart_enabled"`
}

// VLMConfig cấu hình model đọc ẢNH (qwen-vl). CỐ Ý tách khỏi llms.providers:
// loader dựng 1 chat agent cho MỖI entry providers (eino.NewProviderAgents), mà
// VLM không phải agent — nó chỉ đọc ảnh trả text, không tool-calling, không
// quyết định gì. Nhét vào providers sẽ đẻ ra một agent chat rác và model ảnh
// có thể bị chọn nhầm cho câu hỏi chữ.
//
// Enabled=false thì chưa consumer nào được phép dùng (hiện chưa có consumer:
// pipeline extract tự bật theo config worker, xem RAG_RESEARCH.md §6.12).
type VLMConfig struct {
	Enabled     bool    `json:"enabled"`
	BaseURL     string  `json:"base_url"`
	Model       string  `json:"model"`
	APIKey      string  `json:"api_key"`
	TimeoutSec  int     `json:"timeout_sec"`
	MaxTokens   int     `json:"max_tokens"`
	Temperature float64 `json:"temperature"`
	// MaxImageBytes trần ảnh mỗi request; 0 ⇒ mặc định 10MB (ảnh 300 DPI
	// ~1-3MB, base64 phồng thêm ~33%, 10MB là đủ cho 1 trang và chặn được
	// file ai đó nhét ảnh 100 trang vào một request).
	MaxImageBytes int `json:"max_image_bytes"`
}

// Load đọc file JSON và trả về IConfig. opts áp SAU file và SAU env — dùng
// cho flag CLI, nên thứ tự ưu tiên là flag > env > file (xem llm_selection.go).
func Load(path string, opts ...LoadOption) (IConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	var cfg appConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	// Validate cơ bản
	if cfg.App.Port == 0 {
		return nil, fmt.Errorf("app.port is required")
	}
	if cfg.Database.Host == "" {
		return nil, fmt.Errorf("database.host is required")
	}
	if cfg.Embedding.Provider == "" {
		return nil, fmt.Errorf("embedding.provider is required")
	}
	if cfg.Embedding.Model == "" {
		return nil, fmt.Errorf("embedding.model is required")
	}
	if cfg.Embedding.Dim <= 0 {
		return nil, fmt.Errorf("embedding.dim must be > 0")
	}
	// llms là map provider thống nhất — thêm backend mới chỉ cần khai báo
	// trong config (type "openai" cho mọi backend OpenAI-compatible, "ollama"
	// cho Ollama native). Fail-fast khi type lạ để lỗi cấu hình bắt ngay lúc
	// boot; thiếu api_key thì chỉ warn + bỏ qua provider đó (boot vẫn lên,
	// request nào chọn nó mới 400). Key ưu tiên env <TÊN_PROVIDER>_API_KEY
	// (vd GROQ_API_KEY, OPENROUTER_API_KEY) để không commit secret vào file.
	if cfg.LLMs.DefaultProvider == "" {
		cfg.LLMs.DefaultProvider = "ollama"
	}
	cfg.LLMs.DefaultProvider = strings.ToLower(strings.TrimSpace(cfg.LLMs.DefaultProvider))
	for name, p := range cfg.LLMs.Providers {
		switch strings.ToLower(strings.TrimSpace(p.Type)) {
		case LLMTypeOllama, LLMTypeOpenAI:
			p.Type = strings.ToLower(strings.TrimSpace(p.Type))
		default:
			return nil, fmt.Errorf("llms.providers.%s.type phải là %q hoặc %q, got %q",
				name, LLMTypeOllama, LLMTypeOpenAI, p.Type)
		}
		// Cùng Ollama server với embedding là setup phổ biến nhất nên mượn
		// khi base_url bỏ trống (vd local); Ollama Cloud thì khai base_url
		// riêng trong config.
		if p.Type == LLMTypeOllama && p.BaseURL == "" {
			p.BaseURL = cfg.Embedding.BaseURL
		}
		if p.Type == LLMTypeOllama && p.BaseURL == "" {
			p.BaseURL = "http://localhost:11434"
		}
		if envKey := os.Getenv(strings.ToUpper(name) + "_API_KEY"); envKey != "" {
			p.APIKey = envKey
		}
		// RAG cần câu trả lời bám tài liệu nên mặc định 0 (tham lam, ít
		// bịa); >2 là ngoài range OpenAI-compatible thì giữ 0.
		if p.Temperature < 0 || p.Temperature > 2 {
			p.Temperature = 0
		}
		if p.TopP < 0 || p.TopP > 1 {
			p.TopP = 0 // 0 = không gửi, provider tự quyết
		}
		if p.MaxTokens <= 0 {
			p.MaxTokens = 2048
		}
		if p.TimeoutSec <= 0 {
			p.TimeoutSec = 120
		}
		// Kẹp trần 1 giờ — time.Duration(o.TimeoutSec)*time.Second tràn số
		// với giá trị khổng lồ (thành âm rồi rơi về default một cách tình cờ).
		if p.TimeoutSec > 3600 {
			p.TimeoutSec = 3600
		}
		if p.RateLimitRPM < 0 {
			p.RateLimitRPM = 0 // âm là lỗi nhập liệu — coi như không giới hạn
		}
		cfg.LLMs.Providers[name] = p
	}
	// Lớp lựa chọn LLM: env trước rồi mới tới flag (LoadOption) — flag thắng
	// env, env thắng file. Rỗng cả hai lớp thì giữ nguyên config như cũ.
	if err := applyLLMSelection(&cfg, os.Getenv("LLM_PROVIDER"), os.Getenv("LLM_MODEL")); err != nil {
		return nil, err
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	if cfg.Qdrant.Host == "" {
		return nil, fmt.Errorf("qdrant.host is required")
	}
	if cfg.Qdrant.Port == 0 {
		cfg.Qdrant.Port = 6333 // REST mặc định
	}
	if cfg.Qdrant.GrpcPort == 0 {
		cfg.Qdrant.GrpcPort = 6334 // gRPC mặc định (official go-client)
	}
	if cfg.Qdrant.Scheme == "" {
		cfg.Qdrant.Scheme = "http"
	}
	cfg.Qdrant.Scheme = strings.ToLower(strings.TrimSpace(cfg.Qdrant.Scheme))
	if cfg.Qdrant.Scheme != "http" && cfg.Qdrant.Scheme != "https" {
		return nil, fmt.Errorf("qdrant.scheme phải là http hoặc https, got %q", cfg.Qdrant.Scheme)
	}
	// Quantization: fail-fast khi gõ sai để không âm thầm chạy không nén.
	cfg.Qdrant.Quantization = strings.ToLower(strings.TrimSpace(cfg.Qdrant.Quantization))
	switch cfg.Qdrant.Quantization {
	case "", "none":
		cfg.Qdrant.Quantization = ""
	case "scalar", "binary":
	default:
		return nil, fmt.Errorf("qdrant.quantization phải là \"\", \"none\", \"scalar\" hoặc \"binary\", got %q", cfg.Qdrant.Quantization)
	}
	// Oversampling <= 0 là "không khai báo" — Qdrant mặc định 2.0, giữ cùng
	// giá trị để log/config nhìn thấy số thật.
	if cfg.Qdrant.Oversampling <= 0 {
		cfg.Qdrant.Oversampling = 2.0
	}
	// Host dính scheme do copy URL (https://h:6333) thì tách ra để không dựng
	// thành https://https://... — scheme trong host thắng khi config bỏ trống.
	if i := strings.Index(cfg.Qdrant.Host, "://"); i >= 0 {
		if cfg.Qdrant.Scheme == "http" {
			if s := strings.ToLower(cfg.Qdrant.Host[:i]); s == "http" || s == "https" {
				cfg.Qdrant.Scheme = s
			}
		}
		cfg.Qdrant.Host = cfg.Qdrant.Host[i+3:]
	}
	// Token ưu tiên env để production không commit secret vào file.
	if envToken := os.Getenv("QDRANT_TOKEN"); envToken != "" {
		cfg.Qdrant.Token = envToken
	}
	if envBin := os.Getenv("OCR_BINARY"); envBin != "" {
		cfg.OCR.Binary = envBin
	}
	if cfg.OCR.Binary == "" {
		cfg.OCR.Binary = "tesseract" // ăn theo PATH (Linux/prod)
	}
	if cfg.OCR.Lang == "" {
		// Default giữ "vie" chứ không phải "vie+eng": máy chưa cài gói eng
		// sẽ làm tesseract exit != 0 cho MỌI trang (không chỉ kém chính
		// xác). Máy nào đã có eng thì khai "vie+eng" trong config.
		cfg.OCR.Lang = "vie"
	}
	if cfg.OCR.TimeoutSec <= 0 {
		cfg.OCR.TimeoutSec = 120
	}
	// Kẹp trần 1 giờ — time.Duration(o.TimeoutSec)*time.Second tràn số với
	// giá trị khổng lồ (thành âm rồi rơi về default một cách tình cờ).
	if cfg.OCR.TimeoutSec > 3600 {
		cfg.OCR.TimeoutSec = 3600
	}
	if cfg.OCR.RenderDPI <= 0 {
		cfg.OCR.RenderDPI = 300
	}
	if cfg.OCR.MinTextChars < 0 {
		cfg.OCR.MinTextChars = 0
	}
	if cfg.OCR.PSM <= 0 {
		cfg.OCR.PSM = 6
	}
	if cfg.RedisCli.Host == "" {
		return nil, fmt.Errorf("redis.host is required")
	}
	if cfg.Rabbit.Host == "" {
		return nil, fmt.Errorf("rabbitmq.host is required")
	}
	if cfg.Rabbit.Port == 0 {
		cfg.Rabbit.Port = 5672 // AMQP mặc định
	}
	if cfg.Rabbit.User == "" {
		cfg.Rabbit.User = "guest"
	}
	// Password ưu tiên env để production không commit secret vào file
	if envPass := os.Getenv("RABBITMQ_PASSWORD"); envPass != "" {
		cfg.Rabbit.Password = envPass
	}
	if cfg.Rabbit.Vhost == "" {
		cfg.Rabbit.Vhost = "/"
	}
	if cfg.Rabbit.Exchange == "" {
		cfg.Rabbit.Exchange = "knowledge.events"
	}
	if cfg.Rabbit.Queue == "" {
		cfg.Rabbit.Queue = "knowledge.outbox"
	}
	// Secret ưu tiên env để production không phải commit secret vào file
	if envSecret := os.Getenv("JWT_SECRET"); envSecret != "" {
		cfg.JWTCli.Secret = envSecret
	}
	if cfg.JWTCli.Secret == "" {
		return nil, fmt.Errorf("jwt.secret is required (hoặc env JWT_SECRET)")
	}
	if cfg.JWTCli.ExpiryMinutes <= 0 {
		cfg.JWTCli.ExpiryMinutes = 1440 // mặc định 1 ngày
	}
	if cfg.JWTCli.RefreshExpiryDays <= 0 {
		cfg.JWTCli.RefreshExpiryDays = 30
	}
	if cfg.JWTCli.RefreshExpiryDays > 365 {
		cfg.JWTCli.RefreshExpiryDays = 365
	}
	if cfg.JWTCli.RefreshAbsoluteDays <= 0 {
		cfg.JWTCli.RefreshAbsoluteDays = 90
	}
	// Hạn tuyệt đối không được ngắn hơn hạn 1 token, nếu không phiên chết
	// ngay sau lần refresh đầu tiên.
	if cfg.JWTCli.RefreshAbsoluteDays < cfg.JWTCli.RefreshExpiryDays {
		cfg.JWTCli.RefreshAbsoluteDays = cfg.JWTCli.RefreshExpiryDays
	}
	if cfg.JWTCli.RefreshAbsoluteDays > 730 {
		cfg.JWTCli.RefreshAbsoluteDays = 730
	}
	if len(cfg.Cors.AllowedOrigins) == 0 {
		// Mặc định Angular dev — FE gọi thẳng API (không qua dev-server proxy)
		// nên cả hai cách mở dev server phải nằm trong allow list.
		cfg.Cors.AllowedOrigins = []string{"http://localhost:4200", "http://127.0.0.1:4200"}
	}
	if cfg.Worker.TrimIntervalHours <= 0 {
		cfg.Worker.TrimIntervalHours = 24 // mặc định mỗi ngày
	}
	if strings.TrimSpace(cfg.Agent.PromptDir) == "" {
		cfg.Agent.PromptDir = "./prompts" // chạy từ root repo
	}
	switch cfg.Worker.Chunker {
	case "", "recursive", "semantic", "manual":
		if cfg.Worker.Chunker == "" {
			cfg.Worker.Chunker = "recursive" // mặc định tầng 1 bulk
		}
	default:
		return nil, fmt.Errorf("worker.chunker phải là recursive/semantic/manual, got %q", cfg.Worker.Chunker)
	}
	// VLM đọc ảnh: base_url mượn Ollama của embedding khi bỏ trống — cùng
	// setup phổ biến nhất (một server Ollama phục vụ cả embed lẫn model ảnh).
	if cfg.VLM.BaseURL == "" {
		cfg.VLM.BaseURL = cfg.Embedding.BaseURL
	}
	if cfg.VLM.BaseURL == "" {
		cfg.VLM.BaseURL = "http://localhost:11434"
	}
	// Key ưu tiên env như mọi secret khác — không commit key vào file.
	if envKey := os.Getenv("VLM_API_KEY"); envKey != "" {
		cfg.VLM.APIKey = envKey
	}
	// Trích xuất cần tất định: ngoài range thì kéo về 0 thay vì fail.
	if cfg.VLM.Temperature < 0 || cfg.VLM.Temperature > 2 {
		cfg.VLM.Temperature = 0
	}
	if cfg.VLM.TimeoutSec <= 0 {
		cfg.VLM.TimeoutSec = 180 // đọc cả trang ảnh chậm hơn hẳn chat chữ
	}
	if cfg.VLM.TimeoutSec > 3600 {
		cfg.VLM.TimeoutSec = 3600
	}
	if cfg.VLM.MaxTokens <= 0 {
		cfg.VLM.MaxTokens = 2048
	}
	if cfg.VLM.MaxImageBytes <= 0 {
		cfg.VLM.MaxImageBytes = 10 << 20
	}

	return &cfg, nil
}

// Getter methods
func (c *appConfig) GetApp() AppConfig             { return c.App }
func (c *appConfig) GetDatabase() DBConfig         { return c.Database }
func (c *appConfig) GetStorage() S3Config          { return c.Storage }
func (c *appConfig) GetEmbedding() EmbeddingConfig { return c.Embedding }
func (c *appConfig) GetLLMs() LLMsConfig           { return c.LLMs }
func (c *appConfig) GetQdrant() QdrantConfig       { return c.Qdrant }
func (c *appConfig) GetOCR() OCRConfig             { return c.OCR }
func (c *appConfig) GetRedis() RedisConfig         { return c.RedisCli }
func (c *appConfig) GetRabbitMQ() RabbitMQConfig   { return c.Rabbit }
func (c *appConfig) GetJWT() JWTConfig             { return c.JWTCli }
func (c *appConfig) GetCORS() CorsConfig           { return c.Cors }
func (c *appConfig) GetWorker() WorkerConfig       { return c.Worker }
func (c *appConfig) GetAgent() AgentConfig         { return c.Agent }
func (c *appConfig) GetVLM() VLMConfig             { return c.VLM }
