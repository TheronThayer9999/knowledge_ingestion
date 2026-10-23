import { TestBed } from '@angular/core/testing';
import { ActivatedRoute, Router } from '@angular/router';
import { of, Subject, throwError } from 'rxjs';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../core/api/api.models';
import { AuthService } from '../../core/auth/auth.service';
import type { AuthUser } from '../../core/auth/auth.models';
import { MessageService } from '../../shared/feedback/message.service';
import { Login } from './login';

const user: AuthUser = {
  id: 1,
  username: 'demo',
  email: 'demo@example.com',
  phone: '',
  avatar: '',
  active: true,
  role: 0,
  lastLogin: null,
};

describe('Login page', () => {
  const auth = { login: vi.fn() };
  const messages = { success: vi.fn(), errorFromApi: vi.fn() };
  const router = { navigateByUrl: vi.fn(() => Promise.resolve(true)) };
  const queryParamMap = { get: vi.fn((): string | null => null) };
  const route = { snapshot: { queryParamMap } };

  function setup() {
    TestBed.resetTestingModule();
    auth.login.mockReset();
    messages.success.mockClear();
    messages.errorFromApi.mockClear();
    router.navigateByUrl.mockClear();
    queryParamMap.get.mockReset();
    queryParamMap.get.mockReturnValue(null);
    auth.login.mockReturnValue(of(user));
    TestBed.configureTestingModule({
      imports: [Login],
      providers: [
        { provide: AuthService, useValue: auth },
        { provide: MessageService, useValue: messages },
        { provide: Router, useValue: router },
        { provide: ActivatedRoute, useValue: route },
      ],
    });
    const fixture = TestBed.createComponent(Login);
    fixture.detectChanges();
    return { fixture, component: fixture.componentInstance };
  }

  beforeEach(() => {
    auth.login.mockReset();
  });

  it('form rỗng thì không gọi API và đánh dấu touched', () => {
    const { component } = setup();

    component.submit();

    expect(auth.login).not.toHaveBeenCalled();
    expect(component['form'].controls.username.touched).toBe(true);
  });

  it('đăng nhập thành công: gọi API với username đã trim, toast và chuyển trang', async () => {
    const { component } = setup();
    component['form'].setValue({ username: '  demo  ', password: 'secret' });

    component.submit();
    await Promise.resolve();

    expect(auth.login).toHaveBeenCalledWith({ username: 'demo', password: 'secret' });
    expect(messages.success).toHaveBeenCalledWith('AUTH_LOGIN_SUCCESS');
    expect(router.navigateByUrl).toHaveBeenCalledWith('/');
  });

  it('dùng redirect nội bộ sau khi đăng nhập', async () => {
    const { component } = setup();
    queryParamMap.get.mockReturnValue('/articles/5');
    component['form'].setValue({ username: 'demo', password: 'secret' });

    component.submit();
    await Promise.resolve();

    expect(router.navigateByUrl).toHaveBeenCalledWith('/articles/5');
  });

  it('bỏ qua redirect ngoài hệ thống (//evil.com)', async () => {
    const { component } = setup();
    queryParamMap.get.mockReturnValue('//evil.com');
    component['form'].setValue({ username: 'demo', password: 'secret' });

    component.submit();
    await Promise.resolve();

    expect(router.navigateByUrl).toHaveBeenCalledWith('/');
  });

  it('lỗi API thì hiển thị qua MessageService, không chuyển trang', () => {
    const { component } = setup();
    auth.login.mockReturnValue(throwError(() => new ApiError(401, 401, 'sai tài khoản')));
    component['form'].setValue({ username: 'demo', password: 'wrong' });

    component.submit();

    expect(messages.errorFromApi).toHaveBeenCalled();
    expect(router.navigateByUrl).not.toHaveBeenCalled();
  });

  it('đang gửi thì loading bật và chặn submit lần 2', () => {
    const { component } = setup();
    const pending = new Subject<AuthUser>();
    auth.login.mockReturnValue(pending.asObservable());
    component['form'].setValue({ username: 'demo', password: 'secret' });

    component.submit();
    component.submit();

    expect(auth.login).toHaveBeenCalledTimes(1);
    expect(component.loading()).toBe(true);

    pending.next(user);
    pending.complete();
    expect(component.loading()).toBe(false);
  });
});
