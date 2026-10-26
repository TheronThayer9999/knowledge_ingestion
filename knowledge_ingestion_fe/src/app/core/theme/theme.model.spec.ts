import { readFileSync } from 'node:fs';
import {
  DEFAULT_THEME_PALETTE,
  THEME_COLOR_KEYS,
  THEME_PALETTES,
} from './theme.model';

// Đọc thẳng source SCSS (test chạy trong Node qua vitest, cwd = thư mục project)
// — builder unit-test dùng esbuild nên KHÔNG có `?raw` như Vite.
const read = (relative: string): string => readFileSync(relative, 'utf8');
const stylesScss = read('src/styles/styles.scss');
const tokensColorScss = read('src/styles/tokens/_color.scss');

/**
 * Thân của block `.theme-<palette>` trong styles.scss (các block này không
 * lồng nhau nên `[^}]*` là đủ). Trả về null khi palette chưa được khai báo.
 */
function themeBlock(palette: string): string | null {
  const match = stylesScss.match(new RegExp(`\\.theme-${palette}\\s*\\{([^}]*)\\}`));
  return match ? match[1] : null;
}

describe('theme model — mọi theme phải có cùng một bộ key', () => {
  it('mỗi palette có block .theme-<palette> và sinh theme qua mixin app-theme', () => {
    for (const palette of THEME_PALETTES) {
      const block = themeBlock(palette);
      expect(block, `thiếu block .theme-${palette} trong styles.scss`).toBeTruthy();
      expect(block, `.theme-${palette} phải dùng @include app-theme(...)`).toContain(
        '@include app-theme(',
      );
    }
  });

  it('không palette nào tự viết tay --mat-sys-*/--color-* (nguồn gốc lệch bộ key)', () => {
    for (const palette of THEME_PALETTES) {
      const block = themeBlock(palette) ?? '';
      const handWritten = block.match(/--(?:mat-sys|color)-[a-z0-9-]+(?=\s*:)/g) ?? [];
      expect(
        handWritten,
        `.theme-${palette} tự khai báo token: ${handWritten.join(', ')}`,
      ).toEqual([]);
    }
  });

  it(':root gắn liền palette mặc định để chưa kịp gắn class vẫn đủ token', () => {
    // Script chống FOUC chạy trước ThemeService — nếu :root không mang bộ token
    // của palette mặc định thì vài khung hình đầu app không có màu nào.
    const rootBlock = stylesScss.match(/:root\s*,\s*\.theme-([a-z-]+)\s*\{/);
    expect(rootBlock, 'không tìm thấy ":root, .theme-<palette>" trong styles.scss').toBeTruthy();
    expect(rootBlock![1]).toBe(DEFAULT_THEME_PALETTE);
  });

  it('THEME_COLOR_KEYS khớp 1-1 với bộ --mat-sys-* mà token ứng dụng alias', () => {
    const used = [...tokensColorScss.matchAll(/var\(--mat-sys-([a-z0-9-]+)\)/g)].map((m) => m[1]);
    const usedUnique = [...new Set(used)].sort();
    const declared = [...THEME_COLOR_KEYS].sort();

    // thiếu key trong THEME_COLOR_KEYS = app phụ thuộc một token không ai khai
    // báo trong hợp đồng; thừa key = hợp đồng nói dối, palette mới sẽ tưởng
    // phải sinh nhiều hơn mức thật cần.
    expect(usedUnique, 'token màu dùng key chưa có trong THEME_COLOR_KEYS').toEqual(declared);
    expect(new Set(THEME_COLOR_KEYS).size).toBe(THEME_COLOR_KEYS.length);
  });
});
