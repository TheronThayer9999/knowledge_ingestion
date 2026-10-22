import { inject, Injectable } from '@angular/core';
import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Observable, catchError, map, of, switchMap, throwError, timer } from 'rxjs';
import { AuthApiError, AuthErrorCode } from '../models/auth-error.model';
import { LoginRequest, LoginResponse, RegisterRequest, SessionTokens, User } from '../models/auth.model';
import { API_BASE_URL, IAuthService } from './auth.interface';

/**
 * DTO đúng theo JSON wire của backend Go (snake_case) — chỉ dùng nội bộ
 * service này, không rò rỉ sang component. Envelope chung:
 * {status, code, message, data} — data = null khi lỗi.
 */
interface ApiResponse<T> {
  status: boolean;
  code: number;
  message: string;
  data: T | null;
}

interface BackendUser {
  id: number;
  username: string;
  email: string;
  phone: string;
  avatar: string;
  active: boolean;
  role: number;
  last_login?: string;
}

interface BackendSession {
  token: string;
  expires_at: string;
  refresh_token: string;
  refresh_expires_at: string;
}

interface BackendLogin extends BackendSession {
  user: BackendUser;
}

/** HTTP status backend Go → mã nghiệp vụ FE (message server là tiếng Việt, không hiển thị thẳng). */
const HTTP_TO_AUTH_ERROR: Record<number, AuthErrorCode> = {
  401: 'INVALID_CREDENTIALS',
  403: 'ACCOUNT_LOCKED',
  409: 'USERNAME_TAKEN',
};

/**
 * IAuthService nối API thật (cmd/apis, gin + JWT + refresh rotation).
 * Login fail 401, khóa tài khoản 403 — map qua HTTP_TO_AUTH_ERROR.
 * loginWithGoogle: backend CHƯA có endpoint OAuth nên vẫn mock tại chỗ;
 * khi có /auth/google thì thay mỗi hàm này.
 */
@Injectable()
export class ApiAuthService implements IAuthService {
  private readonly http = inject(HttpClient);
  private readonly base = inject(API_BASE_URL);

  login(request: LoginRequest): Observable<LoginResponse> {
    return this.http
      .post<ApiResponse<BackendLogin>>(`${this.base}/auth/login`, { username: request.username, password: request.password })
      .pipe(
        map((res) => this.toLoginResponse(this.unwrap(res))),
        catchError((err) => this.toAuthError(err)),
      );
  }

  /** TODO(API): backend chưa có OAuth Google — giữ hành vi mock để nút login chạy được. */
  loginWithGoogle(): Observable<LoginResponse> {
    const user: User = {
      id: 0,
      username: 'google.user',
      email: 'google.user@gmail.com',
      phone: '',
      avatar: '',
      active: true,
      role: 0,
    };
    return timer(700).pipe(
      map(() => ({
        token: 'google.oauth.placeholder-token',
        expiresAt: new Date(Date.now() + 3600_000).toISOString(),
        refreshToken: 'google.oauth.placeholder-refresh',
        refreshExpiresAt: new Date(Date.now() + 30 * 86400_000).toISOString(),
        user,
      })),
    );
  }

  /** Backend tạo user rồi KHÔNG phát token — phải login lại ngay để có phiên. */
  register(request: RegisterRequest): Observable<LoginResponse> {
    return this.http
      .post<ApiResponse<BackendUser>>(`${this.base}/users`, {
        username: request.username,
        email: request.email,
        password: request.password,
      })
      .pipe(
        switchMap(() => this.login({ username: request.username, password: request.password })),
        catchError((err) => this.toAuthError(err)),
      );
  }

  refresh(refreshToken: string): Observable<SessionTokens> {
    return this.http
      .post<ApiResponse<BackendSession>>(`${this.base}/auth/refresh`, { refresh_token: refreshToken })
      .pipe(
        map((res) => this.toSession(this.unwrap(res))),
        catchError((err) => this.toAuthError(err)),
      );
  }

  logout(refreshToken: string | null): Observable<void> {
    if (!refreshToken) {
      return of(undefined);
    }
    return this.http
      .post<ApiResponse<{ revoked: number }>>(`${this.base}/auth/logout`, { refresh_token: refreshToken })
      .pipe(
        map(() => undefined),
        // Logout là dọn dẹp — server báo gì cũng bỏ qua, client luôn clear state.
        catchError(() => of(undefined)),
      );
  }

  /** data = null không được lọt sang mapper — coi như lỗi lạ. */
  private unwrap<T>(res: ApiResponse<T>): T {
    if (!res.status || res.data === null || res.data === undefined) {
      throw new AuthApiError('GENERIC');
    }
    return res.data;
  }

  private toSession(s: BackendSession): SessionTokens {
    return {
      token: s.token,
      expiresAt: s.expires_at,
      refreshToken: s.refresh_token,
      refreshExpiresAt: s.refresh_expires_at,
    };
  }

  private toLoginResponse(l: BackendLogin): LoginResponse {
    return {
      ...this.toSession(l),
      user: this.toUser(l.user),
    };
  }

  private toUser(u: BackendUser): User {
    return {
      id: u.id,
      username: u.username,
      email: u.email,
      phone: u.phone,
      avatar: u.avatar,
      active: u.active,
      role: u.role,
      lastLogin: u.last_login,
    };
  }

  private toAuthError(err: unknown): Observable<never> {
    if (err instanceof HttpErrorResponse) {
      const code = HTTP_TO_AUTH_ERROR[err.status] ?? 'GENERIC';
      return throwError(() => new AuthApiError(code));
    }
    return throwError(() => err);
  }
}
