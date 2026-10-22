import { Injectable } from '@angular/core';
import { Observable, delay, map, of, throwError } from 'rxjs';
import { AuthApiError } from '../models/auth-error.model';
import { LoginRequest, LoginResponse, User } from '../models/auth.model';
import { IAuthService } from './auth.interface';

/** Tài khoản demo — chỉ dùng khi còn mock, xóa khi nối API thật. */
const DEMO_CREDENTIALS = { username: 'admin', password: 'admin123' };

/**
 * Mock IAuthService — giả lập độ trễ mạng 500ms và sinh JWT giả lập.
 * Đổi sang API thật: viết service khác implements IAuthService (dùng
 * HttpClient gọi /api/v1/auth/...) rồi thay useClass trong app.config.
 */
@Injectable()
export class MockAuthService implements IAuthService {
  login(request: LoginRequest): Observable<LoginResponse> {
    const valid =
      request.username === DEMO_CREDENTIALS.username &&
      request.password === DEMO_CREDENTIALS.password;

    if (!valid) {
      // Trả về mã lỗi — người dùng hiển thị qua i18n ở component.
      return throwError(() => new AuthApiError('INVALID_CREDENTIALS'));
    }

    const user: User = {
      id: crypto.randomUUID(),
      username: DEMO_CREDENTIALS.username,
      displayName: 'Quản trị',
      email: 'admin@example.com',
      roles: ['admin'],
    };

    return of(user).pipe(
      delay(500),
      map((u) => ({ token: this.fakeJwt(u), user: u })),
    );
  }

  logout(): Observable<void> {
    return of(undefined).pipe(delay(200));
  }

  /** Chuỗi giống cấu trúc JWT (header.payload.signature) — chữ ký là giả. */
  private fakeJwt(user: User): string {
    const encode = (obj: object) => btoa(JSON.stringify(obj)).replace(/=+$/, '');
    const payload = {
      sub: user.id,
      username: user.username,
      roles: user.roles,
      exp: Math.floor(Date.now() / 1000) + 60 * 60,
    };
    return `${encode({ alg: 'HS256', typ: 'JWT' })}.${encode(payload)}.mock-signature`;
  }
}
