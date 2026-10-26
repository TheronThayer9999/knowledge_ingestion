import { TestBed } from '@angular/core/testing';
import {
  DEFAULT_THEME_PALETTE,
  THEME_CLASS_PREFIX,
  THEME_PALETTES,
  THEME_STORAGE_KEY,
  ThemeService,
} from './theme.service';

describe('ThemeService', () => {
  const root = document.documentElement;

  beforeEach(() => {
    localStorage.clear();
    root.classList.remove(...THEME_PALETTES.map((palette) => `${THEME_CLASS_PREFIX}${palette}`));
    root.style.removeProperty('color-scheme');
  });

  it('locks the storage-key/class-prefix contract shared with the FOUC script', () => {
    // index.html không import được TS — hai hằng số này là hợp đồng duy nhất
    // giữa script chống FOUC và service, đổi là phải đổi cả hai nơi.
    expect(THEME_STORAGE_KEY).toBe('ki.theme');
    expect(THEME_CLASS_PREFIX).toBe('theme-');
    expect(THEME_PALETTES).toContain(DEFAULT_THEME_PALETTE);
  });

  it('defaults to the azure palette', () => {
    const theme = TestBed.inject(ThemeService);

    expect(theme.palette()).toBe('azure');
    expect(root.classList.contains('theme-azure')).toBe(true);
  });

  it('applies the selected palette class on <html>', () => {
    const theme = TestBed.inject(ThemeService);

    theme.setPalette('rose-red');

    expect(root.classList.contains('theme-rose-red')).toBe(true);
    expect(root.classList.contains('theme-azure')).toBe(false);
  });

  it('toggles mode and updates color-scheme', () => {
    const theme = TestBed.inject(ThemeService);
    const initial = theme.mode();

    theme.toggleMode();

    expect(theme.mode()).not.toBe(initial);
    expect(root.style.getPropertyValue('color-scheme')).toBe(theme.mode());
  });

  it('persists palette and mode to localStorage', () => {
    const theme = TestBed.inject(ThemeService);

    theme.setPalette('cyan-orange');
    theme.setMode('dark');

    expect(JSON.parse(localStorage.getItem(THEME_STORAGE_KEY)!)).toEqual({
      palette: 'cyan-orange',
      mode: 'dark',
    });
  });

  it('restores stored state and ignores invalid values', () => {
    localStorage.setItem(
      THEME_STORAGE_KEY,
      JSON.stringify({ palette: 'magenta-violet', mode: 'dark' }),
    );
    const theme = TestBed.inject(ThemeService);

    expect(theme.palette()).toBe('magenta-violet');
    expect(theme.mode()).toBe('dark');
    expect(root.classList.contains('theme-magenta-violet')).toBe(true);

    // Injector mới để service đọc lại state vừa ghi — giá trị hỏng phải fallback về mặc định
    TestBed.resetTestingModule();
    localStorage.setItem(THEME_STORAGE_KEY, JSON.stringify({ palette: 'nope', mode: 'purple' }));
    const fallback = TestBed.inject(ThemeService);

    expect(THEME_PALETTES).toContain(fallback.palette());
    expect(['light', 'dark']).toContain(fallback.mode());
  });

  it('cleans unknown theme-* classes added by the FOUC script before boot', () => {
    // Storage chứa palette sai định dạng → script FOUC vẫn gắn class `theme-<...>`;
    // service phải dọn hết class theme-* chứ không để sót rác trên <html>.
    root.classList.add(`${THEME_CLASS_PREFIX}palette-khong-ton-tai`);
    localStorage.setItem(
      THEME_STORAGE_KEY,
      JSON.stringify({ palette: 'rose-red', mode: 'light' }),
    );

    const theme = TestBed.inject(ThemeService);

    expect(root.classList.contains(`${THEME_CLASS_PREFIX}palette-khong-ton-tai`)).toBe(false);
    expect(root.classList.contains(`${THEME_CLASS_PREFIX}${theme.palette()}`)).toBe(true);
  });
});
