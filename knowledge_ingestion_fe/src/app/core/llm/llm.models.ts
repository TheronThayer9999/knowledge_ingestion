/**
 * Lựa chọn provider/model cho UI (Đợt 28): backend phản chiếu cấu hình
 * `llms.providers` ra API đọc. API key KHÔNG bao giờ xuống trình duyệt —
 * admin chỉ thấy cờ "đã có key", không thấy giá trị key.
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
  };
}

export function toLlmOptions(dto: LlmOptionsDto): LlmOptions {
  return {
    defaultProvider: dto.default_provider ?? '',
    defaultModel: dto.default_model ?? '',
    providers: (dto.providers ?? []).map(toLlmProviderOption),
  };
}
