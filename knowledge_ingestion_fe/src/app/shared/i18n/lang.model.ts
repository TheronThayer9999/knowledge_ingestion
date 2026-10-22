import VI from './vi.json';

/**
 * Mọi key dịch đều lấy từ vi.json (SCREAMING_SNAKE_CASE, map phẳng key → value).
 * en.json được kiểm qua Record<TranslationKey, string> ở lang.service.ts —
 * thiếu key là lỗi build.
 */
export type TranslationKey = keyof typeof VI;

/** Tham số nội suy trong value: `{{name}}`, `{{username}}`, … */
export interface TranslationParams {
  [param: string]: string | number;
}

/** Model ngôn ngữ — thêm ngôn ngữ mới = thêm file json (đủ bộ key của vi.json) + đăng ký vào MAPS trong lang.service.ts và AVAILABLE_LANGS. */

export type LangName = 'vi' | 'en';

export interface LangOption {
  code: LangName;
  /** Tên tự gọi (endonym) — hiển thị giống nhau ở mọi ngôn ngữ. */
  label: string;
}

export const AVAILABLE_LANGS: readonly LangOption[] = [
  { code: 'vi', label: 'Tiếng Việt' },
  { code: 'en', label: 'English' },
];

export const DEFAULT_LANG: LangName = 'vi';
