import { Injectable, computed, signal } from '@angular/core';
import { safeDocument, safeLocalStorage } from '../browser';
import { DEFAULT_THEME, ThemeName } from '../../../styles/tokens/theme.model';

const THEME_STORAGE_KEY = 'ki.theme';

/**
 * Áp theme runtime bằng cách đổi attribute `data-theme` trên <html> —
 * toàn bộ CSS variables (--app-* và --mat-sys-*) đi theo selector của theme
 * đã được compile sẵn trong material-theme.scss.
 * Lựa chọn lưu localStorage, khôi phục khi tab mới mở.
 */
@Injectable({ providedIn: 'root' })
export class ThemeService {
  private readonly current = signal<ThemeName>(this.restore());

  readonly theme = computed(() => this.current());

  constructor() {
    this.apply(this.current());
  }

  setTheme(name: ThemeName): void {
    this.current.set(name);
    this.apply(name);
    safeLocalStorage()?.setItem(THEME_STORAGE_KEY, name);
  }

  private restore(): ThemeName {
    const saved = safeLocalStorage()?.getItem(THEME_STORAGE_KEY) as ThemeName | null;
    return saved ?? DEFAULT_THEME;
  }

  private apply(name: ThemeName): void {
    const doc = safeDocument();
    if (doc) {
      doc.documentElement.dataset['theme'] = name;
    }
  }
}
