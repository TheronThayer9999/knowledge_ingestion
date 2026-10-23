import { DOCUMENT } from '@angular/common';
import { computed, inject, Injectable, signal } from '@angular/core';
import { EN } from '../../shared/i18n/en';
import { VI, type TranslationKey } from '../../shared/i18n/vi';

export const LANGS = ['vi', 'en'] as const;
export type LangName = (typeof LANGS)[number];

/** Key canonical lấy từ vi.ts — thêm key mới vào vi.ts trước, en.ts thiếu key là lỗi khi nạp */
export type { TranslationKey };

const MAPS: Record<LangName, ReadonlyMap<TranslationKey, string>> = { vi: VI, en: EN };

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
    // buildMessages đã bảo đảm đủ key lúc nạp — fallback chỉ để phòng thủ
    return this.map().get(key) ?? key;
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
