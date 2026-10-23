import { TestBed } from '@angular/core/testing';
import { ActivatedRoute, Router } from '@angular/router';
import { of, Subject, throwError } from 'rxjs';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../core/api/api.models';
import type { AuthUser, RegisterPayload } from '../../core/auth/auth.models';
import { AuthService } from '../../core/auth/auth.service';
import { MessageService } from '../../shared/feedback/message.service';
import { Register } from './register';

const user: AuthUser = {
  id: 7,
  username: 'newbie',
  email: 'newbie@example.com',
  phone: '',
  avatar: '',
  active: true,
  role: 0,
  lastLogin: null,
};

const validValues = {
  username: 'newbie',
  email: 'newbie@example.com',
  phone: '',
  password: 'secret123',
  confirmPassword: 'secret123',
};

describe('Register page', () => {
  const auth = { register: vi.fn() };
  const messages = { success: vi.fn(), errorFromApi: vi.fn() };
  const router = { navigateByUrl: vi.fn(() => Promise.resolve(true)) };

  function setup() {
    TestBed.resetTestingModule();
    auth.register.mockReset();
    messages.success.mockClear();
    messages.errorFromApi.mockClear();
    router.navigateByUrl.mockClear();
    auth.register.mockReturnValue(of(user));
    TestBed.configureTestingModule({
      imports: [Register],
      providers: [
        { provide: AuthService, useValue: auth },
        { provide: MessageService, useValue: messages },
        { provide: Router, useValue: router },
        // RouterLink trong template cần ActivatedRoute
        { provide: ActivatedRoute, useValue: {} },
      ],
    });
    const fixture = TestBed.createComponent(Register);
    fixture.detectChanges();
    return { fixture, component: fixture.componentInstance };
  }

  beforeEach(() => {
    auth.register.mockReset();
  });

  it('form rỗng thì không gọi API và đánh dấu touched', () => {
    const { component } = setup();

    component.submit();

    expect(auth.register).not.toHaveBeenCalled();
    expect(component['form'].controls.username.touched).toBe(true);
  });

  it('chặn submit khi mật khẩu nhập lại không khớp', () => {
    const { component } = setup();
    component['form'].setValue({ ...validValues, confirmPassword: 'khac12345' });

    component.submit();

    expect(component['form'].hasError('passwordsMismatch')).toBe(true);
    expect(auth.register).not.toHaveBeenCalled();
  });

  it('chặn submit khi mật khẩu dưới 8 ký tự', () => {
    const { component } = setup();
    component['form'].setValue({ ...validValues, password: 'short', confirmPassword: 'short' });

    component.submit();

    expect(component['form'].controls.password.hasError('minlength')).toBe(true);
    expect(auth.register).not.toHaveBeenCalled();
  });

  it('đăng ký thành công: trim dữ liệu, toast và chuyển về trang chủ', async () => {
    const { component } = setup();
    component['form'].setValue({ ...validValues, username: '  newbie  ' });

    component.submit();
    await Promise.resolve();

    expect(auth.register).toHaveBeenCalledWith({
      username: 'newbie',
      email: 'newbie@example.com',
      password: 'secret123',
    });
    expect(messages.success).toHaveBeenCalledWith('AUTH_REGISTER_SUCCESS');
    expect(router.navigateByUrl).toHaveBeenCalledWith('/');
  });

  it('chỉ gửi phone khi người dùng nhập', async () => {
    const { component } = setup();
    component['form'].setValue({ ...validValues, phone: '+84901234567' });

    component.submit();
    await Promise.resolve();

    const payload = auth.register.mock.calls[0][0] as RegisterPayload;
    expect(payload.phone).toBe('+84901234567');
  });

  it('phone sai định dạng E.164 thì chặn submit', () => {
    const { component } = setup();
    component['form'].setValue({ ...validValues, phone: '0901234567' });

    component.submit();

    expect(component['form'].controls.phone.hasError('pattern')).toBe(true);
    expect(auth.register).not.toHaveBeenCalled();
  });

  it('lỗi API (trùng username) thì hiển thị qua MessageService, không chuyển trang', () => {
    const { component } = setup();
    auth.register.mockReturnValue(
      throwError(() => new ApiError(400, 400, 'tên đăng nhập đã tồn tại')),
    );
    component['form'].setValue(validValues);

    component.submit();

    expect(messages.errorFromApi).toHaveBeenCalled();
    expect(router.navigateByUrl).not.toHaveBeenCalled();
  });

  it('đang gửi thì loading bật và chặn submit lần 2', () => {
    const { component } = setup();
    const pending = new Subject<AuthUser>();
    auth.register.mockReturnValue(pending.asObservable());
    component['form'].setValue(validValues);

    component.submit();
    component.submit();

    expect(auth.register).toHaveBeenCalledTimes(1);
    expect(component.loading()).toBe(true);

    pending.next(user);
    pending.complete();
    expect(component.loading()).toBe(false);
  });
});
