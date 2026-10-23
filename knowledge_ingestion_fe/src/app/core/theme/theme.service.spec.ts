import { TestBed } from '@angular/core/testing';
import { THEME_PALETTES, ThemeService } from './theme.service';

describe('ThemeService', () => {
  const root = document.documentElement;

  beforeEach(() => {
    localStorage.clear();
    root.classList.remove(...THEME_PALETTES.map((palette) => `theme-${palette}`));
    root.style.removeProperty('color-scheme');
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

    expect(JSON.parse(localStorage.getItem('ki.theme')!)).toEqual({
      palette: 'cyan-orange',
      mode: 'dark',
    });
  });

  it('restores stored state and ignores invalid values', () => {
    localStorage.setItem('ki.theme', JSON.stringify({ palette: 'magenta-violet', mode: 'dark' }));
    const theme = TestBed.inject(ThemeService);

    expect(theme.palette()).toBe('magenta-violet');
    expect(theme.mode()).toBe('dark');
    expect(root.classList.contains('theme-magenta-violet')).toBe(true);

    // Injector mới để service đọc lại state vừa ghi — giá trị hỏng phải fallback về mặc định
    TestBed.resetTestingModule();
    localStorage.setItem('ki.theme', JSON.stringify({ palette: 'nope', mode: 'purple' }));
    const fallback = TestBed.inject(ThemeService);

    expect(THEME_PALETTES).toContain(fallback.palette());
    expect(['light', 'dark']).toContain(fallback.mode());
  });
});
