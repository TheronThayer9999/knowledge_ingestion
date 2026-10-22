/**
 * Model của design token / theme.
 * Mọi theme (SCSS trong thư mục này) bắt buộc có ĐỦ các key dưới đây —
 * thiếu key là thiếu biến CSS và UI vỡ. Thêm key thì phải thêm vào cả
 * interface lẫn cả 3 file token.
 */

export type ThemeName = 'light' | 'dark' | 'brand';

/** Bộ token màu theo M3 system role + token riêng của app. */
export interface ThemeTokens {
  /** Tên token (kebab-case) === key trong file _tokens-*.scss, xuất ra `--app-<token>`. */
  primary: string;
  'on-primary': string;
  'primary-container': string;
  'on-primary-container': string;
  secondary: string;
  'on-secondary': string;
  'secondary-container': string;
  'on-secondary-container': string;
  error: string;
  'on-error': string;
  background: string;
  'on-background': string;
  surface: string;
  'on-surface': string;
  'surface-container': string;
  'surface-container-high': string;
  'surface-container-highest': string;
  'on-surface-variant': string;
  outline: string;
  'outline-variant': string;
  'inverse-surface': string;
  'inverse-on-surface': string;
  'inverse-primary': string;
  radius: string;
}

/** Danh sách theme cho UI switcher — giữ đồng bộ với registry _index.scss. */
export const AVAILABLE_THEMES: ReadonlyArray<{ name: ThemeName; label: string }> = [
  { name: 'light', label: 'Light' },
  { name: 'dark', label: 'Dark' },
  { name: 'brand', label: 'Brand' },
];

export const DEFAULT_THEME: ThemeName = 'light';
