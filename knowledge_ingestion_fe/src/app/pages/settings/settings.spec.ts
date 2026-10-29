import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { MatDialog } from '@angular/material/dialog';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { environment } from '../../../environments/environment';
import { AUTH_STORAGE_KEY } from '../../core/auth/auth-storage.service';
import type { StoredSession } from '../../core/auth/auth.models';
import { LangService } from '../../core/i18n/lang.service';
import { LlmProvidersDialog } from './llm-providers-dialog';
import { MyLlmProvidersDialog } from './my-llm-providers-dialog';
import { Settings } from './settings';

const api = (path: string): string => `${environment.apiBaseUrl}${path}`;

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
  element: HTMLElement;
  http: HttpTestingController;
} {
  localStorage.clear();
  loginAs(role);
  TestBed.resetTestingModule();
  TestBed.configureTestingModule({
    imports: [Settings],
    providers: [provideHttpClient(), provideHttpClientTesting()],
  });
  const fixture = TestBed.createComponent(Settings);
  TestBed.inject(LangService).setLang('vi');
  fixture.detectChanges();
  return {
    fixture,
    element: fixture.nativeElement as HTMLElement,
    http: TestBed.inject(HttpTestingController),
  };
}

function findButton(element: HTMLElement, label: string): HTMLButtonElement {
  const button = Array.from(element.querySelectorAll('button')).find((item) =>
    (item.textContent ?? '').includes(label),
  );
  if (!button) {
    throw new Error(`Không tìm thấy nút "${label}"`);
  }
  return button;
}

describe('Settings — mục Nhà cung cấp AI (Đợt 28)', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('user thường KHÔNG thấy mục và không gọi API', () => {
    const { element, http } = setup(0);

    expect(element.textContent).not.toContain('Nhà cung cấp AI');
    http.expectNone(api('/llm/options'));
    http.verify();
  });

  it('admin thấy mục và nút mở popup, nhưng trang KHÔNG tự nạp provider', () => {
    const { element, http } = setup(1);

    expect(element.textContent).toContain('Nhà cung cấp AI');
    expect(element.textContent).toContain('Quản lý kết nối');
    // Danh sách chỉ nạp khi mở popup — trang cài đặt không tốn lời gọi API
    http.expectNone(api('/llm/options'));
    http.verify();
  });

  it('bấm "Quản lý kết nối" mở popup provider', () => {
    const { element } = setup(1);
    const open = vi
      .spyOn(TestBed.inject(MatDialog), 'open')
      .mockReturnValue({} as unknown as ReturnType<MatDialog['open']>);

    findButton(element, 'Quản lý kết nối').click();

    expect(open).toHaveBeenCalledWith(
      LlmProvidersDialog,
      expect.objectContaining({ maxWidth: '94vw' }),
    );
  });

  it('trang cài đặt không có ô nhập API key — key chỉ nhập trong popup', () => {
    const { element } = setup(1);

    expect(element.querySelector('input[type="password"]')).toBeNull();
    expect(element.querySelectorAll('input').length).toBe(0);
  });
});

describe('Settings — mục Kết nối của tôi (BYOK)', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('user thường CŨNG thấy mục này — mỗi người tự cắm provider riêng', () => {
    const { element, http } = setup(0);

    expect(element.textContent).toContain('Kết nối của tôi');
    expect(element.textContent).toContain('Mở kết nối của tôi');
    // Trang cài đặt không tự nạp danh sách — chỉ nạp khi mở popup
    http.expectNone(api('/llm/my/providers'));
    http.verify();
  });

  it('bấm nút mở popup kết nối riêng', () => {
    const { element } = setup(0);
    const open = vi
      .spyOn(TestBed.inject(MatDialog), 'open')
      .mockReturnValue({} as unknown as ReturnType<MatDialog['open']>);

    findButton(element, 'Mở kết nối của tôi').click();

    expect(open).toHaveBeenCalledWith(
      MyLlmProvidersDialog,
      expect.objectContaining({ maxWidth: '94vw' }),
    );
  });
});
