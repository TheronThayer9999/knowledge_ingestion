import { computed, inject, Injectable, signal } from '@angular/core';
import { Router } from '@angular/router';
import { catchError, firstValueFrom, map, Observable, of, switchMap, tap } from 'rxjs';
import { ApiService } from '../api/api.service';
import { ApiError } from '../api/api.models';
import {
  ROLE_ADMIN,
  ROLE_USER,
  toAuthUser,
  toStoredSession,
  type AuthBroadcast,
  type AuthStatus,
  type AuthUser,
  type LoginCredentials,
  type LoginResponseDto,
  type RefreshResponseDto,
  type RegisterPayload,
  type StoredSession,
  type UserDto,
} from './auth.models';
import { AuthStorageService } from './auth-storage.service';

/**
 * Trạng thái đăng nhập toàn cục (signals) + điều phối token.
 *
 * QUAN TRỌNG — refresh token của backend là single-use, dùng lại sẽ bị
 * coi là bị đánh cắp và thu hồi cả "family". Vì vậy:
 * - Trong tab: `refresh()` giữ 1 promise dùng chung (single-flight),
 *   nhiều request 401 đồng thời chỉ tạo đúng 1 lần gọi /auth/refresh.
 * - Giữa các tab: tab refresh xong phát `LOGIN` (kèm session mới) qua
 *   BroadcastChannel('ki-auth') để tab khác nhận token, không refresh lại;
 *   logout phát `LOGOUT` để mọi tab về trang đăng nhập.
 */
@Injectable({ providedIn: 'root' })
export class AuthService {
  private readonly api = inject(ApiService);
  private readonly storage = inject(AuthStorageService);
  private readonly router = inject(Router);

  private readonly _session = signal<StoredSession | null>(this.storage.read());
  private readonly channel = createAuthChannel();
  private refreshInFlight: Promise<StoredSession> | null = null;

  readonly session = this._session.asReadonly();
  readonly status = computed<AuthStatus>(() => (this._session() ? 'authenticated' : 'anonymous'));
  readonly user = computed(() => this._session()?.user ?? null);
  readonly isAuthenticated = computed(() => this._session() !== null);
  readonly isAdmin = computed(() => (this.user()?.role ?? ROLE_USER) >= ROLE_ADMIN);

  constructor() {
    this.channel?.addEventListener('message', (event: MessageEvent<AuthBroadcast>) => {
      this.handleBroadcast(event.data);
    });
    if (typeof window !== 'undefined') {
      window.addEventListener('storage', (event) => {
        if (event.key === 'ki.auth') {
          this.syncFromStorage();
        }
      });
    }
  }

  login(credentials: LoginCredentials): Observable<AuthUser> {
    return this.api.post<LoginResponseDto>('/auth/login', credentials).pipe(
      tap((dto) => this.applySession(toStoredSession(dto), true)),
      map((dto) => toAuthUser(dto.user)),
    );
  }

  /** Tạo user mới (endpoint công khai) rồi đăng nhập luôn bằng chính tài khoản đó */
  register(payload: RegisterPayload): Observable<AuthUser> {
    const body: Record<string, string> = {
      username: payload.username,
      email: payload.email,
      password: payload.password,
    };
    if (payload.phone) {
      body['phone'] = payload.phone;
    }
    return this.api
      .post<UserDto>('/users', body)
      .pipe(
        switchMap(() => this.login({ username: payload.username, password: payload.password })),
      );
  }

  /**
   * Đổi access token mới — single-flight: request thứ 2 trong cùng tab
   * dùng lại promise đang chạy, tuyệt đối không bắn 2 lần /auth/refresh.
   * Refresh lỗi thì xoá phiên (broadcast LOGOUT) và ném lỗi cho caller.
   */
  refresh(): Promise<StoredSession> {
    if (this.refreshInFlight) {
      return this.refreshInFlight;
    }
    const current = this._session();
    if (!current) {
      return Promise.reject(new ApiError(401, 401, ''));
    }
    this.refreshInFlight = firstValueFrom(
      this.api.post<RefreshResponseDto>('/auth/refresh', { refresh_token: current.refreshToken }),
    )
      .then((dto) => {
        const next: StoredSession = {
          token: dto.token,
          expiresAt: dto.expires_at,
          refreshToken: dto.refresh_token,
          refreshExpiresAt: dto.refresh_expires_at,
          user: current.user,
        };
        this.applySession(next, true);
        return next;
      })
      .catch((error: unknown) => {
        this.clearSession();
        throw error;
      })
      .finally(() => {
        this.refreshInFlight = null;
      });
    return this.refreshInFlight;
  }

  /** Logout phiên hiện tại — luôn xoá local dù API lỗi (best effort) */
  logout(): Observable<unknown> {
    const session = this._session();
    const request$ = session
      ? this.api
          .post<unknown>('/auth/logout', { refresh_token: session.refreshToken })
          .pipe(catchError(() => of(undefined)))
      : of(undefined);
    return request$.pipe(tap(() => this.clearSession()));
  }

  logoutAll(): Observable<unknown> {
    return this.api.post<unknown>('/auth/logout-all').pipe(tap(() => this.clearSession()));
  }

  loadProfile(): Observable<AuthUser> {
    return this.api.get<UserDto>('/auth/profile').pipe(
      tap((dto) => {
        const current = this._session();
        if (current) {
          this.applySession({ ...current, user: toAuthUser(dto) }, false);
        }
      }),
      map((dto) => toAuthUser(dto)),
    );
  }

  /** Đổi mật khẩu xong backend thu hồi mọi phiên → xoá local luôn */
  changePassword(oldPassword: string, newPassword: string): Observable<unknown> {
    return this.api
      .post<unknown>('/auth/change-password', {
        old_password: oldPassword,
        new_password: newPassword,
      })
      .pipe(tap(() => this.clearSession()));
  }

  /** Xoá phiên ở tab này và phát LOGOUT cho các tab khác */
  clearSession(): void {
    this.applySession(null, true);
  }

  /**
   * Áp message từ tab khác. Public để test được; chỉ dùng nội bộ app.
   */
  handleBroadcast(message: AuthBroadcast): void {
    if (message?.type === 'LOGIN' && message.session) {
      this._session.set(message.session);
      this.storage.save(message.session);
      return;
    }
    if (message?.type === 'LOGOUT') {
      this._session.set(null);
      this.storage.clear();
      const redirect = this.router.url;
      void this.router.navigate(['/login'], {
        queryParams:
          redirect && redirect !== '/' && redirect !== '/login' ? { redirect } : undefined,
      });
    }
  }

  private applySession(session: StoredSession | null, broadcast: boolean): void {
    this._session.set(session);
    if (session) {
      this.storage.save(session);
    } else {
      this.storage.clear();
    }
    if (broadcast && this.channel) {
      const message: AuthBroadcast = session ? { type: 'LOGIN', session } : { type: 'LOGOUT' };
      this.channel.postMessage(message);
    }
  }

  private syncFromStorage(): void {
    this._session.set(this.storage.read());
  }
}

function createAuthChannel(): BroadcastChannel | null {
  try {
    return typeof BroadcastChannel === 'undefined' ? null : new BroadcastChannel('ki-auth');
  } catch {
    return null;
  }
}
