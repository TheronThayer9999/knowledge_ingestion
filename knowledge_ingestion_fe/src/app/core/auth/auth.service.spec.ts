import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { of, Subject, throwError } from 'rxjs';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../api/api.models';
import { ApiService } from '../api/api.service';
import type { LoginResponseDto, RefreshResponseDto, StoredSession, UserDto } from './auth.models';
import { AuthService } from './auth.service';
import { AUTH_STORAGE_KEY } from './auth-storage.service';

const userDto: UserDto = {
  id: 1,
  username: 'demo',
  email: 'demo@example.com',
  phone: '',
  avatar: '',
  active: true,
  role: 0,
  last_login: null,
};

function sessionFixture(overrides: Partial<StoredSession> = {}): StoredSession {
  const future = new Date(Date.now() + 24 * 3600 * 1000).toISOString();
  return {
    token: 'access-1',
    expiresAt: future,
    refreshToken: 'refresh-1',
    refreshExpiresAt: future,
    user: {
      id: userDto.id,
      username: userDto.username,
      email: userDto.email,
      phone: userDto.phone,
      avatar: userDto.avatar,
      active: userDto.active,
      role: userDto.role,
      lastLogin: null,
    },
    ...overrides,
  };
}

function loginResponseFixture(): LoginResponseDto {
  const future = new Date(Date.now() + 24 * 3600 * 1000).toISOString();
  return {
    token: 'access-1',
    expires_at: future,
    refresh_token: 'refresh-1',
    refresh_expires_at: future,
    user: userDto,
  };
}

describe('AuthService', () => {
  const api = { get: vi.fn(), post: vi.fn() };
  const router = { url: '/articles', navigate: vi.fn(() => Promise.resolve(true)) };

  function setup(session?: StoredSession): AuthService {
    localStorage.clear();
    if (session) {
      localStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify(session));
    }
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({
      providers: [
        { provide: ApiService, useValue: api },
        { provide: Router, useValue: router },
      ],
    });
    return TestBed.inject(AuthService);
  }

  beforeEach(() => {
    api.get.mockReset();
    api.post.mockReset();
    router.navigate.mockClear();
  });

  it('hydrate phiên từ localStorage khi khởi tạo', () => {
    const service = setup(sessionFixture());

    expect(service.isAuthenticated()).toBe(true);
    expect(service.user()?.username).toBe('demo');
    expect(service.status()).toBe('authenticated');
  });

  it('bỏ qua phiên có refresh token quá hạn', () => {
    const service = setup(
      sessionFixture({ refreshExpiresAt: new Date(Date.now() - 1000).toISOString() }),
    );

    expect(service.isAuthenticated()).toBe(false);
    expect(localStorage.getItem(AUTH_STORAGE_KEY)).toBeNull();
  });

  it('login lưu phiên (cả user) và trả user đã map', () => {
    const service = setup();
    api.post.mockReturnValue(of(loginResponseFixture()));

    let username = '';
    service.login({ username: 'demo', password: 'secret123' }).subscribe((user) => {
      username = user.username;
    });

    expect(username).toBe('demo');
    expect(service.isAuthenticated()).toBe(true);
    expect(service.session()?.refreshToken).toBe('refresh-1');
    expect(localStorage.getItem(AUTH_STORAGE_KEY)).toContain('refresh-1');
  });

  it('register tạo user rồi tự đăng nhập và lưu phiên', () => {
    const service = setup();
    api.post.mockImplementation((path: string) =>
      path === '/users' ? of(userDto) : of(loginResponseFixture()),
    );

    let username = '';
    service
      .register({ username: 'newbie', email: 'new@example.com', password: 'secret123' })
      .subscribe((user) => (username = user.username));

    expect(api.post).toHaveBeenNthCalledWith(1, '/users', {
      username: 'newbie',
      email: 'new@example.com',
      password: 'secret123',
    });
    expect(api.post).toHaveBeenNthCalledWith(2, '/auth/login', {
      username: 'newbie',
      password: 'secret123',
    });
    expect(username).toBe('demo');
    expect(service.isAuthenticated()).toBe(true);
  });

  it('register chỉ gửi phone khi có giá trị', () => {
    const service = setup();
    api.post.mockImplementation((path: string) =>
      path === '/users' ? of(userDto) : of(loginResponseFixture()),
    );

    service
      .register({
        username: 'newbie',
        email: 'new@example.com',
        password: 'secret123',
        phone: '+84901234567',
      })
      .subscribe();

    expect(api.post).toHaveBeenNthCalledWith(1, '/users', {
      username: 'newbie',
      email: 'new@example.com',
      password: 'secret123',
      phone: '+84901234567',
    });
  });

  it('refresh là single-flight: 2 lần gọi chỉ tạo 1 request', async () => {
    const service = setup(sessionFixture());
    const subject = new Subject<RefreshResponseDto>();
    api.post.mockReturnValue(subject.asObservable());

    const first = service.refresh();
    const second = service.refresh();

    expect(first).toBe(second);
    expect(api.post).toHaveBeenCalledTimes(1);
    expect(api.post).toHaveBeenCalledWith('/auth/refresh', { refresh_token: 'refresh-1' });

    const future = new Date(Date.now() + 3600 * 1000).toISOString();
    subject.next({
      token: 'access-2',
      expires_at: future,
      refresh_token: 'refresh-2',
      refresh_expires_at: future,
    });
    subject.complete();

    await expect(first).resolves.toMatchObject({ token: 'access-2', refreshToken: 'refresh-2' });
    await expect(second).resolves.toMatchObject({ token: 'access-2' });
    expect(service.session()?.token).toBe('access-2');
    expect(service.session()?.user.username).toBe('demo');
  });

  it('refresh lỗi thì xoá phiên', async () => {
    const service = setup(sessionFixture());
    api.post.mockReturnValue(throwError(() => new ApiError(401, 401, '')));

    await expect(service.refresh()).rejects.toBeInstanceOf(ApiError);

    expect(service.isAuthenticated()).toBe(false);
    expect(localStorage.getItem(AUTH_STORAGE_KEY)).toBeNull();
  });

  it('logout gửi refresh_token và xoá phiên dù API lỗi', () => {
    const service = setup(sessionFixture());
    api.post.mockReturnValue(throwError(() => new ApiError(500, 500, 'boom')));

    let completed = false;
    service.logout().subscribe({ complete: () => (completed = true) });

    expect(api.post).toHaveBeenCalledWith('/auth/logout', { refresh_token: 'refresh-1' });
    expect(completed).toBe(true);
    expect(service.isAuthenticated()).toBe(false);
  });

  it('logoutAll xoá phiên sau khi gọi API', () => {
    const service = setup(sessionFixture());
    api.post.mockReturnValue(of({ revoked: 2 }));

    service.logoutAll().subscribe();

    expect(api.post).toHaveBeenCalledWith('/auth/logout-all');
    expect(service.isAuthenticated()).toBe(false);
  });

  it('loadProfile cập nhật user trong phiên', () => {
    const service = setup(sessionFixture());
    api.get.mockReturnValue(of({ ...userDto, username: 'renamed' }));

    service.loadProfile().subscribe();

    expect(service.user()?.username).toBe('renamed');
    expect(localStorage.getItem(AUTH_STORAGE_KEY)).toContain('renamed');
  });

  it('changePassword gửi đúng payload và xoá phiên', () => {
    const service = setup(sessionFixture());
    api.post.mockReturnValue(of(null));

    service.changePassword('old-pass', 'new-pass').subscribe();

    expect(api.post).toHaveBeenCalledWith('/auth/change-password', {
      old_password: 'old-pass',
      new_password: 'new-pass',
    });
    expect(service.isAuthenticated()).toBe(false);
  });

  it('handleBroadcast LOGOUT xoá phiên và đưa về /login kèm redirect', () => {
    const service = setup(sessionFixture());

    service.handleBroadcast({ type: 'LOGOUT' });

    expect(service.isAuthenticated()).toBe(false);
    expect(router.navigate).toHaveBeenCalledWith(['/login'], {
      queryParams: { redirect: '/articles' },
    });
  });

  it('handleBroadcast LOGIN nhận phiên mới từ tab khác', () => {
    const service = setup();
    const incoming = sessionFixture({ token: 'access-tab-2', refreshToken: 'refresh-tab-2' });

    service.handleBroadcast({ type: 'LOGIN', session: incoming });

    expect(service.isAuthenticated()).toBe(true);
    expect(service.session()?.token).toBe('access-tab-2');
    expect(localStorage.getItem(AUTH_STORAGE_KEY)).toContain('access-tab-2');
  });
});
