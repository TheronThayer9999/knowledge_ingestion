import { DOCUMENT } from '@angular/common';
import { computed, inject, Injectable, signal } from '@angular/core';

/**
 * Nguồn DUY NHẤT của danh sách palette. Script chống FOUC trong index.html
 * cố ý KHÔNG lặp lại danh sách này — nó chỉ áp định dạng `theme-<palette>`,
 * giá trị lạ không có rule CSS tương ứng sẽ tự rơi về palette mặc định của
 * `:root` (xem styles.scss), nên thêm palette chỉ cần sửa 2 chỗ: mảng này +
 * block `.theme-<palette>` trong styles.scss.
 */
export const THEME_PALETTES = ['azure', 'rose-red', 'magenta-violet', 'cyan-orange'] as const;

export type ThemePalette = (typeof THEME_PALETTES)[number];
export type ThemeMode = 'light' | 'dark';

interface ThemeState {
  palette: ThemePalette;
  mode: ThemeMode;
}

// Hợp đồng giữa ThemeService và script FOUC trong index.html
export const THEME_STORAGE_KEY = 'ki.theme';
export const THEME_CLASS_PREFIX = 'theme-';
export const DEFAULT_THEME_PALETTE: ThemePalette = 'azure';

@Injectable({ providedIn: 'root' })
export class ThemeService {
  private readonly document = inject(DOCUMENT);
  private readonly state = signal<ThemeState>(readStoredState());

  readonly palette = computed(() => this.state().palette);
  readonly mode = computed(() => this.state().mode);

  constructor() {
    this.apply(this.state());
  }

  setPalette(palette: ThemePalette): void {
    this.update({ ...this.state(), palette });
  }

  setMode(mode: ThemeMode): void {
    this.update({ ...this.state(), mode });
  }

  toggleMode(): void {
    this.setMode(this.mode() === 'dark' ? 'light' : 'dark');
  }

  private update(state: ThemeState): void {
    this.state.set(state);
    this.persist(state);
    this.apply(state);
  }

  private apply(state: ThemeState): void {
    const root = this.document.documentElement;
    // Dọn MỌI class `theme-*` (kể cả class lạ do script FOUC gắn từ storage hỏng),
    // rồi mới gắn palette hiện hành — tránh sót class rác trên <html>.
    Array.from(root.classList)
      .filter((cls) => cls.startsWith(THEME_CLASS_PREFIX))
      .forEach((cls) => root.classList.remove(cls));
    root.classList.add(`${THEME_CLASS_PREFIX}${state.palette}`);
    root.style.colorScheme = state.mode;
  }

  private persist(state: ThemeState): void {
    try {
      localStorage.setItem(THEME_STORAGE_KEY, JSON.stringify(state));
    } catch {
      // localStorage có thể bị chặn (private mode) — không được làm sập app vì chuyện này
    }
  }
}

function readStoredState(): ThemeState {
  const systemMode: ThemeMode =
    typeof window !== 'undefined' && window.matchMedia?.('(prefers-color-scheme: dark)').matches
      ? 'dark'
      : 'light';
  const fallback: ThemeState = { palette: DEFAULT_THEME_PALETTE, mode: systemMode };

  try {
    const stored = JSON.parse(
      localStorage.getItem(THEME_STORAGE_KEY) ?? 'null',
    ) as Partial<ThemeState> | null;
    if (!stored) {
      return fallback;
    }
    return {
      palette: THEME_PALETTES.includes(stored.palette as ThemePalette)
        ? (stored.palette as ThemePalette)
        : fallback.palette,
      mode: stored.mode === 'dark' || stored.mode === 'light' ? stored.mode : fallback.mode,
    };
  } catch {
    return fallback;
  }
}
