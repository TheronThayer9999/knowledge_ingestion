import { Injectable, computed, signal } from '@angular/core';
import { browserLangPrefix, safeDocument, safeLocalStorage } from '../browser';
import { EN } from './en';
import { AVAILABLE_LANGS, DEFAULT_LANG, LangName, TranslationKey, TranslationParams } from './lang.model';
import { VI } from './vi';

const LANG_STORAGE_KEY = 'ki.lang';

/** Map ngôn ngữ → bảng dịch (thêm ngôn ngữ mới = thêm file map + 1 dòng ở đây). */
const MAPS: Record<LangName, Record<TranslationKey, string>> = {
  vi: VI,
  en: EN,
};

/**
 * i18n tối giản, signal-based: mỗi ngôn ngữ là một map phẳng key → value.
 * Nơi cần chỉ gọi `lang.t('login.title')` (kèm params nội suy nếu value có {{var}}).
 * Ưu tiên ngôn ngữ: lựa chọn đã lưu (localStorage) → navigator → DEFAULT_LANG.
 * Đổi language runtime: setLang() — mọi template đọc t() cập nhật ngay vì là signal.
 */
@Injectable({ providedIn: 'root' })
export class LangService {
  private readonly current = signal<LangName>(this.restore());

  readonly lang = computed(() => this.current());
  readonly langs = AVAILABLE_LANGS;

  constructor() {
    this.applyHtmlLang(this.current());
  }

  /** Tra key theo ngôn ngữ hiện tại; value thiếu rơi về DEFAULT_LANG, vẫn thiếu thì trả chính key. */
  t(key: TranslationKey, params?: TranslationParams): string {
    const value = MAPS[this.current()][key] || MAPS[DEFAULT_LANG][key] || key;
    return this.interpolate(value, params);
  }

  setLang(lang: LangName): void {
    if (lang === this.current()) {
      return;
    }
    this.current.set(lang);
    this.applyHtmlLang(lang);
    safeLocalStorage()?.setItem(LANG_STORAGE_KEY, lang);
  }

  private applyHtmlLang(lang: LangName): void {
    const doc = safeDocument();
    if (doc) {
      doc.documentElement.lang = lang;
    }
  }

  /** Thay {{name}} bằng params.name — không có param thì thay bằng rỗng. */
  private interpolate(value: string, params?: TranslationParams): string {
    if (!params) {
      return value;
    }
    return value.replace(/\{\{(\w+)\}\}/g, (_, name: string) => String(params[name] ?? ''));
  }

  private restore(): LangName {
    const saved = safeLocalStorage()?.getItem(LANG_STORAGE_KEY);
    if (saved && AVAILABLE_LANGS.some((l) => l.code === saved)) {
      return saved as LangName;
    }
    const navLang = browserLangPrefix();
    return (AVAILABLE_LANGS.find((l) => l.code === navLang)?.code ?? DEFAULT_LANG) as LangName;
  }
}
