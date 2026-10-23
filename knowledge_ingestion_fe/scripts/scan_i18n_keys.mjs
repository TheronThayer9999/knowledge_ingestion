// Quét toàn bộ key i18n dạng 'SCREAMING_SNAKE' được tham chiếu trong src/app (ts + html + json nền),
// in ra danh sách unique để đối chiếu với vi.json/en.json. Chạy: node scripts/scan_i18n_keys.mjs
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

const root = join(process.cwd(), 'src', 'app');
const keys = new Set();
const pattern = /'([A-Z][0-9A-Z]*(?:_[0-9A-Z]+)+)'/g;
// loại các literal không phải key i18n hay gặp
const ignore = new Set(['API_BASE_URL', 'USER_STORAGE_KEY', 'LANG_STORAGE_KEY', 'THEME_STORAGE_KEY', 'dd/MM/yyyy HH:mm']);

function walk(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p);
    else if (/\.(ts|html)$/.test(name)) {
      const src = readFileSync(p, 'utf8');
      for (const m of src.matchAll(pattern)) {
        if (!ignore.has(m[1])) keys.add(m[1]);
      }
    }
  }
}
walk(root);

const vi = new Set(Object.keys(JSON.parse(readFileSync(join(root, 'shared/i18n/vi.json'), 'utf8'))));
const used = [...keys].sort();
console.log('== MISSING_IN_VI_JSON ==');
console.log(used.filter((k) => !vi.has(k)).join('\n'));
console.log('== UNUSED_IN_JSON (có trong vi.json nhưng code không tham chiếu) ==');
console.log([...vi].filter((k) => !keys.has(k)).join('\n'));
