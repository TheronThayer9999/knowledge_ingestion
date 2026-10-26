/**
 * Hợp đồng THEME của app (nguồn duy nhất — ThemeService chỉ là hành vi).
 *
 * Mô hình: 4 palette × 2 chế độ sáng/tối.
 * - palette = class `theme-<palette>` gắn lên <html> (styles.scss khai báo
 *   block tương ứng, mỗi block gọi `@include app-theme(...)`);
 * - sáng/tối = `color-scheme` trên <html>, token M3 sinh ra từ mat.theme()
 *   đều là `light-dark(...)` nên CÙNG một bộ key chạy cho cả hai chế độ.
 *
 * Vì sao cần file này: "theme thiếu key" là loại lỗi nhìn không ra — thiếu
 * một biến CSS thì control chỉ rơi về nền/trong suốt, app không báo lỗi.
 * theme.model.spec.ts khoá lại hai bất biến:
 *   1) mọi palette phải có block `.theme-<palette>` và block đó phải sinh
 *      theme qua mixin `app-theme` (không tự viết tay từng --mat-sys-*);
 *   2) mọi key M3 mà token ứng dụng alias phải có tên trong THEME_COLOR_KEYS.
 */

export const THEME_PALETTES = ['azure', 'rose-red', 'magenta-violet', 'cyan-orange'] as const;

export type ThemePalette = (typeof THEME_PALETTES)[number];
export type ThemeMode = 'light' | 'dark';

export interface ThemeState {
  palette: ThemePalette;
  mode: ThemeMode;
}

export const DEFAULT_THEME_PALETTE: ThemePalette = 'azure';

// Hợp đồng giữa ThemeService và script chống FOUC trong index.html
export const THEME_STORAGE_KEY = 'ki.theme';
export const THEME_CLASS_PREFIX = 'theme-';

/**
 * Bộ key M3 mà token ứng dụng (`src/styles/tokens/_color.scss`) alias vào.
 * mat.theme() sinh đủ bộ này cho MỌI palette — đây là danh sách những gì
 * app thực sự phụ thuộc, không phải toàn bộ hệ token M3.
 */
export const THEME_COLOR_KEYS = [
  'primary',
  'on-primary',
  'surface',
  'on-surface',
  'on-surface-variant',
  'error',
  'surface-container-lowest',
  'surface-container-low',
  'surface-container',
  'surface-container-high',
  'outline',
  'outline-variant',
  'primary-container',
  'on-primary-container',
  'secondary-container',
  'on-secondary-container',
  'tertiary-container',
  'on-tertiary-container',
  'error-container',
  'on-error-container',
] as const;

export type ThemeColorKey = (typeof THEME_COLOR_KEYS)[number];
