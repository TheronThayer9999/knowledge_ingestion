import { Injectable } from '@angular/core';
import { Observable, delay, map, of, throwError } from 'rxjs';
import { AuthApiError } from '../models/auth-error.model';
import { LoginRequest, LoginResponse, RegisterRequest, SessionTokens, User } from '../models/auth.model';
import { IAuthService } from './auth.interface';

/** Tài khoản demo — chỉ dùng khi còn mock, xóa khi nối API thật. */
const DEMO_CREDENTIALS = { username: 'admin', password: 'admin123' };

/**
 * Mock IAuthService — giữ để test/spec và để quay lại chế độ offline khi
 * backend chết. Bản production wire trong app.config là ApiAuthService.
 * Mô phỏng đủ rotation: refresh() sinh cặp mới, token cũ không dùng lại.
 */
@Injectable()
export class MockAuthService implements IAuthService {
  /** Bộ tên đăng nhập đã tồn tại — seed sẵn admin, register trùng thì báo lỗi. */
  private readonly takenUsernames = new Set<string>([DEMO_CREDENTIALS.username]);
  /** Refresh token đang sống — bản cũ bị vô hiệu sau lần refresh đầu. */
  private liveRefreshToken: string | null = null;

  login(request: LoginRequest): Observable<LoginResponse> {
    const valid =
      request.username === DEMO_CREDENTIALS.username &&
      request.password === DEMO_CREDENTIALS.password;

    if (!valid) {
      // Trả về mã lỗi — người dùng hiển thị qua i18n ở component.
      return throwError(() => new AuthApiError('INVALID_CREDENTIALS'));
    }

    const user: User = {
      id: 1,
      username: DEMO_CREDENTIALS.username,
      email: 'admin@example.com',
      phone: '',
      avatar: '',
      active: true,
      role: 1,
    };

    return of(user).pipe(delay(500), map((u) => this.issueSession(u)));
  }

  /** Mock OAuth Google: giả lập OAuth đã thành công, trả user Google cố định. */
  loginWithGoogle(): Observable<LoginResponse> {
    const user: User = {
      id: 2,
      username: 'google.user',
      email: 'google.user@gmail.com',
      phone: '',
      avatar: '',
      active: true,
      role: 0,
    };
    return of(user).pipe(delay(700), map((u) => this.issueSession(u)));
  }

  register(request: RegisterRequest): Observable<LoginResponse> {
    if (this.takenUsernames.has(request.username)) {
      return throwError(() => new AuthApiError('USERNAME_TAKEN'));
    }
    this.takenUsernames.add(request.username);

    const user: User = {
      id: this.takenUsernames.size + 10,
      username: request.username,
      email: request.email,
      phone: '',
      avatar: '',
      active: true,
      role: 0,
    };
    return of(user).pipe(delay(500), map((u) => this.issueSession(u)));
  }

  refresh(refreshToken: string): Observable<SessionTokens> {
    if (!this.liveRefreshToken || refreshToken !== this.liveRefreshToken) {
      return throwError(() => new AuthApiError('INVALID_CREDENTIALS'));
    }
    const rotated = this.fakeSession();
    this.liveRefreshToken = rotated.refreshToken;
    return of(rotated).pipe(delay(300));
  }

  logout(refreshToken: string | null): Observable<void> {
    if (refreshToken && refreshToken === this.liveRefreshToken) {
      this.liveRefreshToken = null;
    }
    return of(undefined).pipe(delay(200));
  }

  private issueSession(user: User): LoginResponse {
    const session = this.fakeSession();
    this.liveRefreshToken = session.refreshToken;
    return { ...session, user };
  }

  private fakeSession(): SessionTokens {
    const encode = (obj: object) => btoa(JSON.stringify(obj)).replace(/=+$/, '');
    const now = Math.floor(Date.now() / 1000);
    const payload = { sub: 'mock', exp: now + 60 * 60 };
    return {
      // Chuỗi giống cấu trúc JWT (header.payload.signature) — chữ ký là giả.
      token: `${encode({ alg: 'HS256', typ: 'JWT' })}.${encode(payload)}.mock-signature`,
      expiresAt: new Date((now + 3600) * 1000).toISOString(),
      refreshToken: crypto.randomUUID(),
      refreshExpiresAt: new Date((now + 30 * 86400) * 1000).toISOString(),
    };
  }
}
