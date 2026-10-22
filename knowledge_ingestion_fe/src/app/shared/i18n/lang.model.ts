import { VI } from './vi';

/** Mọi key dịch đều lấy từ VI — không tự phát sinh key lẻ ở nơi sử dụng. */
export type TranslationKey = keyof typeof VI;

/** Tham số nội suy trong value: `{{name}}`, `{{username}}`, … */
export interface TranslationParams {
  [param: string]: string | number;
}

/** Model ngôn ngữ — thêm ngôn ngữ mới = thêm file map phẳng (vd fr.ts, Record<TranslationKey, string>) + đăng ký vào MAPS trong lang.service.ts và AVAILABLE_LANGS. */

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
