import { TestBed } from '@angular/core/testing';
import { beforeEach, describe, expect, it } from 'vitest';
import type { StoredSession } from './auth.models';
import { AUTH_STORAGE_KEY, AuthStorageService } from './auth-storage.service';

function sessionFixture(overrides: Partial<StoredSession> = {}): StoredSession {
  const future = new Date(Date.now() + 24 * 3600 * 1000).toISOString();
  return {
    token: 'access-1',
    expiresAt: future,
    refreshToken: 'refresh-1',
    refreshExpiresAt: future,
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
    ...overrides,
  };
}

describe('AuthStorageService', () => {
  let storage: AuthStorageService;

  beforeEach(() => {
    TestBed.resetTestingModule();
    localStorage.clear();
    storage = TestBed.inject(AuthStorageService);
  });

  it('trả về null khi chưa có phiên', () => {
    expect(storage.read()).toBeNull();
  });

  it('đọc lại phiên đã lưu', () => {
    const session = sessionFixture();
    storage.save(session);

    expect(storage.read()).toEqual(session);
  });

  it('xoá phiên khi JSON hỏng', () => {
    localStorage.setItem(AUTH_STORAGE_KEY, '{not-json');

    expect(storage.read()).toBeNull();
    expect(localStorage.getItem(AUTH_STORAGE_KEY)).toBeNull();
  });

  it('từ chối phiên thiếu trường bắt buộc', () => {
    localStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify({ token: 'a' }));

    expect(storage.read()).toBeNull();
  });

  it('từ chối phiên có refresh token quá hạn', () => {
    storage.save(sessionFixture({ refreshExpiresAt: new Date(Date.now() - 1000).toISOString() }));

    expect(storage.read()).toBeNull();
    expect(localStorage.getItem(AUTH_STORAGE_KEY)).toBeNull();
  });

  it('clear xoá key khỏi localStorage', () => {
    storage.save(sessionFixture());
    storage.clear();

    expect(localStorage.getItem(AUTH_STORAGE_KEY)).toBeNull();
  });
});
