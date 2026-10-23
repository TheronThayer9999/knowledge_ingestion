import { DOCUMENT } from '@angular/common';
import { computed, inject, Injectable, signal } from '@angular/core';

/** Các palette M3 hỗ trợ — mỗi palette sinh token --mat-sys-* dưới class `.theme-<palette>` */
export const THEME_PALETTES = ['azure', 'rose-red', 'magenta-violet', 'cyan-orange'] as const;

export type ThemePalette = (typeof THEME_PALETTES)[number];
export type ThemeMode = 'light' | 'dark';

interface ThemeState {
  palette: ThemePalette;
  mode: ThemeMode;
}

const STORAGE_KEY = 'ki.theme';

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
    root.classList.remove(...THEME_PALETTES.map((palette) => `theme-${palette}`));
    root.classList.add(`theme-${state.palette}`);
    root.style.colorScheme = state.mode;
  }

  private persist(state: ThemeState): void {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
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
  const fallback: ThemeState = { palette: 'azure', mode: systemMode };

  try {
    const stored = JSON.parse(
      localStorage.getItem(STORAGE_KEY) ?? 'null',
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
