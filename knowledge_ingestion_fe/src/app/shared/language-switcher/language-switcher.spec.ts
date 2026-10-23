import { TestBed } from '@angular/core/testing';
import { LangService } from '../../core/i18n/lang.service';
import { LanguageSwitcher } from './language-switcher';

describe('LanguageSwitcher', () => {
  beforeEach(async () => {
    localStorage.clear();
    await TestBed.configureTestingModule({
      imports: [LanguageSwitcher],
    }).compileComponents();
  });

  it('should create', () => {
    const fixture = TestBed.createComponent(LanguageSwitcher);
    expect(fixture.componentInstance).toBeTruthy();
  });

  it('should render the language button with the active language label', () => {
    const fixture = TestBed.createComponent(LanguageSwitcher);
    fixture.detectChanges();

    const button = fixture.nativeElement.querySelector('button') as HTMLButtonElement;

    expect(button.getAttribute('aria-label')).toBe('Đổi ngôn ngữ');
  });

  it('should switch language through the service', () => {
    const fixture = TestBed.createComponent(LanguageSwitcher);
    const lang = TestBed.inject(LangService);

    lang.setLang('en');
    expect(lang.lang()).toBe('en');
    expect(lang.t('TOOLBAR_CHANGE_LANGUAGE')).toBe('Change language');

    fixture.detectChanges();
    const button = fixture.nativeElement.querySelector('button') as HTMLButtonElement;
    expect(button.getAttribute('aria-label')).toBe('Change language');
  });
});
