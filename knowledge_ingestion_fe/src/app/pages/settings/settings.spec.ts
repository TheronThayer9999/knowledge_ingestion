import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { beforeEach, describe, expect, it } from 'vitest';
import { environment } from '../../../environments/environment';
import { AUTH_STORAGE_KEY } from '../../core/auth/auth-storage.service';
import type { StoredSession } from '../../core/auth/auth.models';
import { LangService } from '../../core/i18n/lang.service';
import { Settings } from './settings';

const api = (path: string): string => `${environment.apiBaseUrl}${path}`;

const ENVELOPE_OK = { status: true, code: 0, message: 'success' };

const OPTIONS_DTO = {
  default_provider: '9router',
  default_model: 'nemotron',
  providers: [
    {
      slug: '9router',
      label: '9Router',
      type: 'openai',
      default_model: 'nemotron',
      models: ['nemotron'],
      available: true,
      thinking: true,
      is_default: true,
      base_url: 'http://localhost:20128/v1',
      has_key: true,
    },
    {
      slug: 'groq',
      label: 'Groq',
      type: 'openai',
      default_model: 'llama-3.3-70b',
      available: false,
      base_url: 'https://api.groq.com',
      has_key: false,
    },
  ],
};

/** Phiên đăng nhập giả với role cho trước (0 = user, 1 = admin) */
function loginAs(role: number): void {
  const future = new Date(Date.now() + 24 * 3600 * 1000).toISOString();
  const session: StoredSession = {
    token: 'access-1',
    expiresAt: future,
    refreshToken: 'refresh-1',
    refreshExpiresAt: future,
    user: {
      id: 1,
      username: role === 1 ? 'admin' : 'demo',
      email: 'demo@example.com',
      phone: '',
      avatar: '',
      active: true,
      role,
      lastLogin: null,
    },
  };
  localStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify(session));
}

function setup(role: number): {
  fixture: ComponentFixture<Settings>;
  http: HttpTestingController;
  lang: LangService;
} {
  localStorage.clear();
  loginAs(role);
  TestBed.resetTestingModule();
  TestBed.configureTestingModule({
    imports: [Settings],
    providers: [provideHttpClient(), provideHttpClientTesting()],
  });
  const fixture = TestBed.createComponent(Settings);
  const lang = TestBed.inject(LangService);
  lang.setLang('vi');
  fixture.detectChanges();
  return { fixture, http: TestBed.inject(HttpTestingController), lang };
}

describe('Settings — card Nhà cung cấp AI (Đợt 28)', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('user thường KHÔNG thấy card và không gọi API', () => {
    const { fixture, http } = setup(0);
    const element = fixture.nativeElement as HTMLElement;

    expect(element.textContent).not.toContain('Nhà cung cấp AI');
    http.expectNone(api('/llm/options'));
    http.verify();
  });

  it('admin thấy card, gọi API và in danh sách provider', () => {
    const { fixture, http } = setup(1);
    const element = fixture.nativeElement as HTMLElement;

    expect(element.textContent).toContain('Nhà cung cấp AI');
    const req = http.expectOne(api('/llm/options'));
    req.flush({ ...ENVELOPE_OK, data: OPTIONS_DTO });
    fixture.detectChanges();

    expect(element.textContent).toContain('9Router');
    expect(element.textContent).toContain('9router');
    expect(element.textContent).toContain('Mặc định');
    expect(element.textContent).toContain('Sẵn sàng');
    http.verify();
  });

  it('provider chưa sẵn sàng vẫn hiện (admin cần thấy cái gì hỏng)', () => {
    const { fixture, http } = setup(1);
    const element = fixture.nativeElement as HTMLElement;

    http.expectOne(api('/llm/options')).flush({ ...ENVELOPE_OK, data: OPTIONS_DTO });
    fixture.detectChanges();

    expect(element.textContent).toContain('Groq');
    expect(element.textContent).toContain('Chưa sẵn sàng');
    expect(element.textContent).toContain('Chưa có key');
    expect(element.textContent).toContain('Đã có key');
    http.verify();
  });

  it('base URL chỉ hiện khi backend trả (user thường không có field này)', () => {
    const { fixture, http } = setup(1);
    const element = fixture.nativeElement as HTMLElement;

    http.expectOne(api('/llm/options')).flush({ ...ENVELOPE_OK, data: OPTIONS_DTO });
    fixture.detectChanges();

    expect(element.textContent).toContain('http://localhost:20128/v1');
    http.verify();
  });

  it('API lỗi thì báo lỗi, không vỡ trang', () => {
    const { fixture, http } = setup(1);
    const element = fixture.nativeElement as HTMLElement;

    http.expectOne(api('/llm/options')).flush(
      { ...ENVELOPE_OK, status: false, code: 500, message: 'lỗi' },
      { status: 500, statusText: 'Server Error' },
    );
    fixture.detectChanges();

    expect(element.textContent).toContain('Không tải được danh sách nhà cung cấp AI.');
    http.verify();
  });

  it('không có ô nhập API key — key nằm trong file cấu hình', () => {
    const { fixture, http } = setup(1);
    const element = fixture.nativeElement as HTMLElement;

    http.expectOne(api('/llm/options')).flush({ ...ENVELOPE_OK, data: OPTIONS_DTO });
    fixture.detectChanges();

    expect(element.querySelector('input[type="password"]')).toBeNull();
    expect(element.querySelectorAll('input').length).toBe(0);
    expect(element.textContent).toContain('configs/config.json');
    http.verify();
  });
});
