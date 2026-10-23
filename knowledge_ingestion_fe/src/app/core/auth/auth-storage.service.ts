import { Injectable } from '@angular/core';
import type { StoredSession } from './auth.models';

export const AUTH_STORAGE_KEY = 'ki.auth';

/**
 * Lưu phiên trong localStorage['ki.auth'] dưới dạng 1 JSON.
 * Đánh đổi: token truy cập được từ JS nên dính rủi ro XSS — chấp nhận để
 * giữ đăng nhập qua reload; không lưu bất kỳ bí mật nào khác.
 * Phiên có refresh token quá hạn bị coi như không tồn tại.
 */
@Injectable({ providedIn: 'root' })
export class AuthStorageService {
  read(): StoredSession | null {
    let raw: string | null = null;
    try {
      raw = localStorage.getItem(AUTH_STORAGE_KEY);
    } catch {
      return null;
    }
    if (!raw) {
      return null;
    }
    try {
      const parsed: unknown = JSON.parse(raw);
      if (!isStoredSession(parsed)) {
        this.clear();
        return null;
      }
      const refreshExpires = Date.parse(parsed.refreshExpiresAt);
      if (!Number.isFinite(refreshExpires) || refreshExpires <= Date.now()) {
        this.clear();
        return null;
      }
      return parsed;
    } catch {
      this.clear();
      return null;
    }
  }

  save(session: StoredSession): void {
    try {
      localStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify(session));
    } catch {
      // localStorage bị chặn (private mode) — phiên chỉ sống trong RAM
    }
  }

  clear(): void {
    try {
      localStorage.removeItem(AUTH_STORAGE_KEY);
    } catch {
      // bỏ qua
    }
  }
}

function isStoredSession(value: unknown): value is StoredSession {
  if (typeof value !== 'object' || value === null) {
    return false;
  }
  const session = value as Partial<StoredSession>;
  const user = session.user as Partial<StoredSession['user']> | undefined;
  return (
    typeof session.token === 'string' &&
    session.token.length > 0 &&
    typeof session.refreshToken === 'string' &&
    session.refreshToken.length > 0 &&
    typeof session.expiresAt === 'string' &&
    typeof session.refreshExpiresAt === 'string' &&
    typeof user === 'object' &&
    user !== null &&
    typeof user.id === 'number' &&
    typeof user.username === 'string'
  );
}
