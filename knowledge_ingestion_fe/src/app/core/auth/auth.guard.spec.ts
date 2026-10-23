import { TestBed } from '@angular/core/testing';
import { type ActivatedRouteSnapshot, Router, type RouterStateSnapshot } from '@angular/router';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { MessageService } from '../../shared/feedback/message.service';
import { AuthService } from './auth.service';
import { adminGuard, authGuard, guestGuard } from './auth.guard';

describe('auth guards', () => {
  const router = {
    createUrlTree: vi.fn((commands: unknown[], extras?: unknown) => ({ commands, extras })),
  };
  const messages = { error: vi.fn() };

  function setup(options: { authenticated: boolean; admin?: boolean }): {
    auth: { isAuthenticated: ReturnType<typeof vi.fn>; isAdmin: ReturnType<typeof vi.fn> };
  } {
    TestBed.resetTestingModule();
    router.createUrlTree.mockClear();
    messages.error.mockClear();
    const auth = {
      isAuthenticated: vi.fn(() => options.authenticated),
      isAdmin: vi.fn(() => options.admin ?? false),
    };
    TestBed.configureTestingModule({
      providers: [
        { provide: AuthService, useValue: auth },
        { provide: Router, useValue: router },
        { provide: MessageService, useValue: messages },
      ],
    });
    return { auth };
  }

  const run = (guard: typeof authGuard, url = '/articles') =>
    TestBed.runInInjectionContext(() =>
      guard({} as ActivatedRouteSnapshot, { url } as RouterStateSnapshot),
    );

  beforeEach(() => router.createUrlTree.mockClear());

  it('authGuard cho qua khi đã đăng nhập', () => {
    setup({ authenticated: true });
    expect(run(authGuard)).toBe(true);
  });

  it('authGuard chuyển hướng /login kèm redirect', () => {
    setup({ authenticated: false });

    run(authGuard, '/articles');

    expect(router.createUrlTree).toHaveBeenCalledWith(['/login'], {
      queryParams: { redirect: '/articles' },
    });
  });

  it('guestGuard chặn người đã đăng nhập về trang chủ', () => {
    setup({ authenticated: true });

    run(guestGuard);

    expect(router.createUrlTree).toHaveBeenCalledWith(['/']);
  });

  it('guestGuard cho khách vào /login', () => {
    setup({ authenticated: false });
    expect(run(guestGuard)).toBe(true);
  });

  it('adminGuard cho admin đi tiếp', () => {
    setup({ authenticated: true, admin: true });
    expect(run(adminGuard)).toBe(true);
  });

  it('adminGuard báo lỗi và về trang chủ khi thiếu quyền', () => {
    setup({ authenticated: true, admin: false });

    run(adminGuard);

    expect(messages.error).toHaveBeenCalledWith('AUTH_ERROR_FORBIDDEN');
    expect(router.createUrlTree).toHaveBeenCalledWith(['/']);
  });

  it('adminGuard đưa khách chưa đăng nhập về /login', () => {
    setup({ authenticated: false });

    run(adminGuard);

    expect(router.createUrlTree).toHaveBeenCalledWith(['/login']);
  });
});
