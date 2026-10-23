import { TestBed } from '@angular/core/testing';
import { ThemeService } from '../../core/theme/theme.service';
import { ThemeSwitcher } from './theme-switcher';

describe('ThemeSwitcher', () => {
  beforeEach(async () => {
    localStorage.clear();
    await TestBed.configureTestingModule({
      imports: [ThemeSwitcher],
    }).compileComponents();
  });

  it('should create', () => {
    const fixture = TestBed.createComponent(ThemeSwitcher);
    expect(fixture.componentInstance).toBeTruthy();
  });

  it('should toggle mode when clicking the mode button', () => {
    const fixture = TestBed.createComponent(ThemeSwitcher);
    fixture.detectChanges();

    const theme = TestBed.inject(ThemeService);
    const button = fixture.nativeElement.querySelector(
      'button[aria-label="Chuyển sang chế độ tối"]',
    ) as HTMLButtonElement;

    button.click();

    expect(theme.mode()).toBe('dark');
  });
});
