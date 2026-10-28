/**
 * Lựa chọn provider/model cho UI (Đợt 28): backend phản chiếu cấu hình
 * `llms.providers` + các provider admin tạo trong popup ra API đọc. API key
 * KHÔNG bao giờ xuống trình duyệt — admin chỉ thấy cờ "đã có key", không thấy
 * giá trị key.
 */

export interface LlmProviderOption {
  slug: string;
  /** Tên hiển thị; backend đã fallback về slug khi config không khai label */
  label: string;
  /** Protocol client: "ollama" | "openai" */
  type: string;
  defaultModel: string;
  /** Model gợi ý cho menu chọn (luôn là mảng, có thể rỗng) */
  models: string[];
  /** Loader đã dựng được agent cho provider này (key/cấu hình hợp lệ) */
  available: boolean;
  /** Có client suy luận sâu riêng */
  thinking: boolean;
  isDefault: boolean;
  /** chỉ admin */
  baseUrl?: string;
  /** chỉ admin */
  hasKey?: boolean;
  /** chỉ admin — key đang lấy từ "config" hay "database" ('' = chưa có key) */
  keySource?: LlmKeySource;
  /** chỉ admin — model mặc định khai trong configs/config.json */
  configModel?: string;
  /**
   * chỉ admin — provider có khai báo trong configs/config.json không.
   * true: "xoá" chỉ gỡ phần ghi đè, provider quay về file.
   * false: provider do UI tạo, xoá là mất hẳn.
   */
  fromConfig?: boolean;
  /** chỉ admin — admin tắt provider (vẫn hiện trong popup để bật lại) */
  disabled?: boolean;
}

/** Nguồn của api key đang có hiệu lực (backend trả, không phải giá trị key) */
export type LlmKeySource = 'config' | 'database';

/**
 * Loại provider chọn trong form. Backend chỉ có 2 khuôn client nhưng admin đọc
 * tài liệu Groq/OpenRouter/NVIDIA dưới tên "openai-compatible" — gửi nguyên văn
 * tên đó, server chuẩn hoá về "openai" (NormalizeProviderType).
 */
export type LlmProviderKind = 'ollama' | 'openai-compatible';

/** Đảo ngược chuẩn hoá của server để form hiện lại đúng tên admin đã chọn. */
export function toProviderKind(type: string): LlmProviderKind {
  return type === 'ollama' ? 'ollama' : 'openai-compatible';
}

/** Body PUT /llm/providers/{slug} — field bỏ trống nghĩa là "giữ như cũ". */
export interface LlmProviderForm {
  label?: string;
  type?: LlmProviderKind;
  base_url?: string;
  api_key?: string;
  disabled?: boolean;
}

export interface LlmOptions {
  defaultProvider: string;
  defaultModel: string;
  providers: LlmProviderOption[];
}

// ---- DTO khớp JSON backend (snake_case) ----

export interface LlmProviderOptionDto {
  slug: string;
  label?: string;
  type?: string;
  default_model?: string;
  models?: string[];
  available?: boolean;
  thinking?: boolean;
  is_default?: boolean;
  base_url?: string;
  has_key?: boolean;
  key_source?: string;
  config_model?: string;
  from_config?: boolean;
  disabled?: boolean;
}

export interface LlmOptionsDto {
  default_provider?: string;
  default_model?: string;
  providers?: LlmProviderOptionDto[];
}

export function toLlmProviderOption(dto: LlmProviderOptionDto): LlmProviderOption {
  return {
    slug: dto.slug,
    label: dto.label || dto.slug,
    type: dto.type ?? '',
    defaultModel: dto.default_model ?? '',
    models: dto.models ?? [],
    available: dto.available === true,
    thinking: dto.thinking === true,
    isDefault: dto.is_default === true,
    // Field admin-only: backend không trả cho user thường nên vẫn undefined
    baseUrl: dto.base_url,
    hasKey: dto.has_key,
    keySource:
      dto.key_source === 'config' || dto.key_source === 'database' ? dto.key_source : undefined,
    configModel: dto.config_model,
    fromConfig: dto.from_config === true,
    disabled: dto.disabled === true,
  };
}

export function toLlmOptions(dto: LlmOptionsDto): LlmOptions {
  return {
    defaultProvider: dto.default_provider ?? '',
    defaultModel: dto.default_model ?? '',
    providers: (dto.providers ?? []).map(toLlmProviderOption),
  };
}

/** Kết quả "Tải model" từ provider — danh sách backend đã lưu vào DB */
export interface LlmModelsDto {
  slug?: string;
  models?: string[];
}
