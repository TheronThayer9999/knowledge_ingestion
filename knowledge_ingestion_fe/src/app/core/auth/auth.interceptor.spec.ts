import { HttpClient, provideHttpClient, withInterceptors } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { MessageService } from '../../shared/feedback/message.service';
import type { StoredSession } from './auth.models';
import { AuthService } from './auth.service';
import { authInterceptor } from './auth.interceptor';
import { AUTH_STORAGE_KEY } from './auth-storage.service';

const future = () => new Date(Date.now() + 24 * 3600 * 1000).toISOString();

function sessionFixture(): StoredSession {
  return {
    token: 'access-1',
    expiresAt: future(),
    refreshToken: 'refresh-1',
    refreshExpiresAt: future(),
    user: {
      id: 1,
      username: 'demo',
      email: 'demo@example.com',
      phone: '',
      avatar: '',
      active: true,
      role: 0,
      lastLogin: null,
    },
  };
}

const envelope = (code: number, message: string) => ({
  status: false,
  code,
  message,
  data: null,
});

const tick = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

describe('authInterceptor', () => {
  const router = { url: '/articles', navigate: vi.fn(() => Promise.resolve(true)) };
  const messages = { error: vi.fn() };
  let http: HttpTestingController;
  let client: HttpClient;
  let auth: AuthService;

  function setup(session?: StoredSession): void {
    localStorage.clear();
    if (session) {
      localStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify(session));
    }
    router.navigate.mockClear();
    messages.error.mockClear();
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(withInterceptors([authInterceptor])),
        provideHttpClientTesting(),
        { provide: Router, useValue: router },
        { provide: MessageService, useValue: messages },
      ],
    });
    http = TestBed.inject(HttpTestingController);
    client = TestBed.inject(HttpClient);
    auth = TestBed.inject(AuthService);
  }

  beforeEach(() => {
    setup(sessionFixture());
  });

  it('gắn Bearer token khi đã đăng nhập', () => {
    client.get('/api/v1/articles').subscribe();

    const req = http.expectOne('/api/v1/articles');
    expect(req.request.headers.get('Authorization')).toBe('Bearer access-1');
    req.flush({ status: true, code: 0, message: 'success', data: [] });
  });

  it('không gắn token cho /auth/login', () => {
    client.post('/api/v1/auth/login', {}).subscribe();

    const req = http.expectOne('/api/v1/auth/login');
    expect(req.request.headers.has('Authorization')).toBe(false);
    req.flush({ status: true, code: 0, message: 'success', data: null });
  });

  it('2 request 401 đồng thời chỉ tạo 1 lần refresh rồi retry cả hai', async () => {
    client.get('/api/v1/a').subscribe({ error: () => undefined });
    client.get('/api/v1/b').subscribe({ error: () => undefined });

    http.expectOne('/api/v1/a').flush(envelope(401, 'expired'), {
      status: 401,
      statusText: 'Unauthorized',
    });
    http.expectOne('/api/v1/b').flush(envelope(401, 'expired'), {
      status: 401,
      statusText: 'Unauthorized',
    });

    const refresh = http.expectOne('/api/v1/auth/refresh');
    expect(refresh.request.body).toEqual({ refresh_token: 'refresh-1' });
    refresh.flush({
      status: true,
      code: 0,
      message: 'success',
      data: {
        token: 'access-2',
        expires_at: future(),
        refresh_token: 'refresh-2',
        refresh_expires_at: future(),
      },
    });
    await tick();

    const retried = http.match('/api/v1/a').concat(http.match('/api/v1/b'));
    expect(retried).toHaveLength(2);
    for (const req of retried) {
      expect(req.request.headers.get('Authorization')).toBe('Bearer access-2');
      req.flush({ status: true, code: 0, message: 'success', data: null });
    }

    expect(auth.session()?.token).toBe('access-2');
    expect(auth.session()?.refreshToken).toBe('refresh-2');
    expect(localStorage.getItem(AUTH_STORAGE_KEY)).toContain('refresh-2');
    http.expectNone('/api/v1/auth/refresh');
  });

  it('refresh thất bại → xoá phiên, báo hết hạn và về /login', async () => {
    client.get('/api/v1/articles').subscribe({ error: () => undefined });

    http.expectOne('/api/v1/articles').flush(envelope(401, 'expired'), {
      status: 401,
      statusText: 'Unauthorized',
    });
    await tick();

    http
      .expectOne('/api/v1/auth/refresh')
      .flush(envelope(401, 'phiên không hợp lệ'), { status: 401, statusText: 'Unauthorized' });
    await tick();

    expect(auth.isAuthenticated()).toBe(false);
    expect(messages.error).toHaveBeenCalledWith('AUTH_SESSION_EXPIRED');
    expect(router.navigate).toHaveBeenCalledWith(['/login'], {
      queryParams: { redirect: '/articles' },
    });
  });

  it('401 từ /auth/login không kích hoạt refresh', async () => {
    client.post('/api/v1/auth/login', {}).subscribe({ error: () => undefined });

    http.expectOne('/api/v1/auth/login').flush(envelope(401, 'sai tài khoản hoặc mật khẩu'), {
      status: 401,
      statusText: 'Unauthorized',
    });
    await tick();

    http.expectNone('/api/v1/auth/refresh');
    expect(messages.error).not.toHaveBeenCalled();
    expect(router.navigate).not.toHaveBeenCalled();
  });

  it('403 không kích hoạt refresh', async () => {
    client.get('/api/v1/admin/queue').subscribe({ error: () => undefined });

    http.expectOne('/api/v1/admin/queue').flush(envelope(403, 'không có quyền'), {
      status: 403,
      statusText: 'Forbidden',
    });
    await tick();

    http.expectNone('/api/v1/auth/refresh');
    expect(router.navigate).not.toHaveBeenCalled();
  });
});
