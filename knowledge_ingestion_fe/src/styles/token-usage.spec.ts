import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';

/**
 * Hợp đồng dùng token (không hardcode trong component):
 * - chữ phải lấy `--text-style-*` (xem src/styles/tokens/_typography.scss),
 *   không dùng role M3 `--mat-sys-title/body/label/...` vì role đó có thang
 *   riêng (title-large 400 22px) lệch với thang của app (22px/500);
 * - không viết `font-size: <n>px` / `font-weight: <n>` rời rạc — muốn cỡ khác
 *   thì thêm token, không thêm số;
 * - bo góc luôn đi qua `--radius-*`; hình dáng control Material chỉ khai báo ở
 *   src/styles/_material.scss — component khai báo lại là nguồn design phân đôi.
 * Chỉ cho phép cỡ tương đối (`em`) trong khối markdown vì nó bám theo chữ gốc.
 */
const TOKENS_DIR = join('src', 'styles', 'tokens');
const MATERIAL_FILE = join('src', 'styles', '_material.scss');
const APP_DIR = join('src', 'app');
const SCSS_FILES: string[] = [];

function walk(dir: string): void {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) {
      walk(full);
    } else if (entry.endsWith('.scss') && full !== join(TOKENS_DIR, '_typography.scss')) {
      SCSS_FILES.push(full);
    }
  }
}
walk(join('src', 'styles'));
walk(join('src', 'app'));

function sources(): { path: string; text: string }[] {
  return SCSS_FILES.filter((path) => path !== MATERIAL_FILE).map((path) => ({
    path,
    text: readFileSync(path, 'utf8'),
  }));
}

function componentSources(): { path: string; text: string }[] {
  return SCSS_FILES.filter((path) => path.startsWith(APP_DIR)).map((path) => ({
    path,
    text: readFileSync(path, 'utf8'),
  }));
}

describe('token usage trong SCSS', () => {
  it('không dùng role typography của M3 trực tiếp — dùng --text-style-*', () => {
    const offenders = sources()
      .filter((src) => /--mat-sys-(headline|display|title|body|label)-[a-z]/.test(src.text))
      .map((src) => src.path);

    expect(offenders, `còn dùng role M3 ở: ${offenders.join(', ')}`).toEqual([]);
  });

  it('không hardcode font-size px / font-weight số trong component', () => {
    const offenders = sources().flatMap((src) => {
      const hits = src.text.match(/font-size:\s*\d+px|font-weight:\s*\d+/g) ?? [];
      return hits.map((hit) => `${src.path}: ${hit}`);
    });

    expect(offenders, `còn chữ hardcode ở:\n${offenders.join('\n')}`).toEqual([]);
  });

  it('bo góc lấy từ --radius-* (không viết px tay; 50% cho hình tròn)', () => {
    const offenders = sources().flatMap((src) => {
      const hits = [...src.text.matchAll(/border-radius:\s*([^;\n]+)/g)]
        .map((m) => m[1].trim())
        .filter((value) => !/^(var\(--radius|calc\(|50%)/.test(value))
        .map((value) => `${src.path}: border-radius: ${value}`);
      return hits;
    });

    expect(offenders, `bo góc hardcode ở:\n${offenders.join('\n')}`).toEqual([]);
  });

  it('component không khai báo lại container-shape của Material', () => {
    const offenders = componentSources().flatMap((src) => {
      const hits =
        src.text.match(/--mat-[a-z-]*container-shape[^;]*/g)?.map((hit) => `${src.path}: ${hit}`) ??
        [];
      return hits;
    });

    expect(
      offenders,
      `hình dáng control phải sửa ở _material.scss, không ở component:\n${offenders.join('\n')}`,
    ).toEqual([]);
  });
});
