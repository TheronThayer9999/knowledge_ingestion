import { DOCUMENT } from '@angular/common';
import { computed, inject, Injectable, signal } from '@angular/core';
import EN from '../../shared/i18n/en.json';
import VI from '../../shared/i18n/vi.json';

export const LANGS = ['vi', 'en'] as const;
export type LangName = (typeof LANGS)[number];

/** Key canonical lấy từ vi.json — thêm key mới vào vi.json trước, en.json thiếu key là lỗi build */
export type TranslationKey = keyof typeof VI;

// en.json được kiểm tra cấu trúc: thiếu key nào so với vi.json là TypeScript báo lỗi
const MAPS: Record<LangName, Record<TranslationKey, string>> = { vi: VI, en: EN };

const STORAGE_KEY = 'ki.lang';
const DEFAULT_LANG: LangName = 'vi';

@Injectable({ providedIn: 'root' })
export class LangService {
  private readonly document = inject(DOCUMENT);
  private readonly state = signal<LangName>(readStoredLang());
  private readonly map = computed(() => MAPS[this.state()]);

  readonly lang = this.state.asReadonly();

  constructor() {
    this.applyAttribute(this.state());
  }

  /** Dịch theo key; đọc signal bên trong nên template tự cập nhật khi đổi ngôn ngữ */
  t(key: TranslationKey): string {
    return this.map()[key];
  }

  setLang(lang: LangName): void {
    this.state.set(lang);
    this.applyAttribute(lang);
    this.persist(lang);
  }

  toggleLang(): void {
    this.setLang(this.state() === 'vi' ? 'en' : 'vi');
  }

  private applyAttribute(lang: LangName): void {
    this.document.documentElement.lang = lang;
  }

  private persist(lang: LangName): void {
    try {
      localStorage.setItem(STORAGE_KEY, lang);
    } catch {
      // localStorage có thể bị chặn (private mode) — bỏ qua
    }
  }
}

function readStoredLang(): LangName {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    return LANGS.includes(stored as LangName) ? (stored as LangName) : DEFAULT_LANG;
  } catch {
    return DEFAULT_LANG;
  }
}
