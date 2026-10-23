import { TestBed } from '@angular/core/testing';
import { LangService } from './lang.service';

describe('LangService', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('defaults to vietnamese', () => {
    const lang = TestBed.inject(LangService);

    expect(lang.lang()).toBe('vi');
    expect(lang.t('WELCOME_TITLE')).toBe('Xin chào, Knowledge Ingestion');
  });

  it('switches language and persists the choice', () => {
    const lang = TestBed.inject(LangService);

    lang.setLang('en');

    expect(lang.lang()).toBe('en');
    expect(lang.t('WELCOME_TITLE')).toBe('Hello, Knowledge Ingestion');
    expect(localStorage.getItem('ki.lang')).toBe('en');
  });

  it('toggles between vi and en', () => {
    const lang = TestBed.inject(LangService);

    lang.toggleLang();
    expect(lang.lang()).toBe('en');

    lang.toggleLang();
    expect(lang.lang()).toBe('vi');
  });

  it('updates the html lang attribute', () => {
    const lang = TestBed.inject(LangService);

    lang.setLang('en');
    expect(document.documentElement.lang).toBe('en');
  });

  it('restores the stored language and falls back on invalid values', () => {
    localStorage.setItem('ki.lang', 'en');
    const lang = TestBed.inject(LangService);
    expect(lang.lang()).toBe('en');

    // Injector mới để service đọc lại state vừa ghi
    TestBed.resetTestingModule();
    localStorage.setItem('ki.lang', 'fr');
    const fallback = TestBed.inject(LangService);
    expect(fallback.lang()).toBe('vi');
  });
});
