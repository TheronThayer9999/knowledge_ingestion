import { TestBed } from '@angular/core/testing';
import { LangService } from './core/i18n/lang.service';
import { App } from './app';

describe('App', () => {
  beforeEach(async () => {
    localStorage.clear();
    await TestBed.configureTestingModule({
      imports: [App],
    }).compileComponents();
  });

  it('should create the app', () => {
    const fixture = TestBed.createComponent(App);
    const app = fixture.componentInstance;
    expect(app).toBeTruthy();
  });

  it('should render the translated welcome title', async () => {
    const fixture = TestBed.createComponent(App);
    const lang = TestBed.inject(LangService);
    await fixture.whenStable();
    const compiled = fixture.nativeElement as HTMLElement;

    expect(compiled.querySelector('mat-card-title')?.textContent).toContain(
      lang.t('WELCOME_TITLE'),
    );
    expect(compiled.querySelector('mat-card-title')?.textContent).toContain('Xin chào');
  });
});
