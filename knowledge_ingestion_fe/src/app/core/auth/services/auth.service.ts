import { Injectable, computed, inject, signal } from '@angular/core';
import { Observable, finalize, shareReplay, tap, throwError } from 'rxjs';
import { safeLocalStorage } from '../../../shared/browser';
import { MessageService } from '../../../shared/feedback/message.service';
import { AuthState, LoginRequest, LoginResponse, RegisterRequest, SessionTokens, User } from '../models/auth.model';
import { AUTH_CHANNEL_NAME, AuthEvent } from '../models/auth-event.model';
import { AUTH_SERVICE, IAuthService } from './auth.interface';

const TOKEN_STORAGE_KEY = 'ki.auth.token';
const REFRESH_STORAGE_KEY = 'ki.auth.refresh_token';
const USER_STORAGE_KEY = 'ki.auth.user';

/**
 * Store auth phía client: giữ AuthState trong signal, persist localStorage,
 * và ĐỒNG BỘ LOGIN/LOGOUT/REFRESH GIỮA CÁC TAB qua BroadcastChannel.
 * Gọi backend qua IAuthService (InjectionToken AUTH_SERVICE).
 *
 * Refresh token ROTATION mỗi lần đổi — tab nào cũng giữ bản mới nhất qua
 * broadcast, tránh tab khác dùng token cũ làm server nghi reuse và thu hồi
 * cả family (401 dây chuyền).
 */
@Injectable({ providedIn: 'root' })
export class AuthService {
  private readonly api = inject<IAuthService>(AUTH_SERVICE);
  private readonly messages = inject(MessageService);
  /** ID riêng của tab này, dùng để lọc message do chính tab gửi ra. */
  private readonly tabId = crypto.randomUUID();
  private readonly channel: BroadcastChannel | null =
    typeof BroadcastChannel !== 'undefined' ? new BroadcastChannel(AUTH_CHANNEL_NAME) : null;

  private readonly state = signal<AuthState>(this.restore());

  /** Nhiều request 401 đồng thời chỉ được phép kích hoạt MỘT lượt refresh. */
  private pendingRefresh: Observable<SessionTokens> | null = null;

  readonly user = computed(() => this.state().user);
  readonly token = computed(() => this.state().token);
  readonly isAuthenticated = computed(() => this.state().status === 'authenticated');

  constructor() {
    if (this.channel) {
      this.channel.onmessage = (event: MessageEvent<AuthEvent>) => this.onRemoteEvent(event.data);
    }
  }

  login(request: LoginRequest): Observable<LoginResponse> {
    return this.withSession(this.api.login(request));
  }

  loginWithGoogle(): Observable<LoginResponse> {
    return this.withSession(this.api.loginWithGoogle());
  }

  register(request: RegisterRequest): Observable<LoginResponse> {
    return this.withSession(this.api.register(request));
  }

  /**
   * Đổi cặp token mới bằng refresh token đang lưu; in-flight dedupe cho
   * nhiều request 401 cùng lúc. Thành công thì cập nhật store + broadcast
   * để các tab khác không dùng lại token cũ.
   */
  refreshSession(): Observable<SessionTokens> {
    if (this.pendingRefresh) {
      return this.pendingRefresh;
    }
    const refreshToken = this.state().refreshToken;
    if (!refreshToken) {
      return throwError(() => new Error('no refresh token'));
    }
    this.pendingRefresh = this.api.refresh(refreshToken).pipe(
      tap((session) => {
        this.applyTokens(session);
        const user = this.state().user;
        if (user) {
          this.broadcast({
            type: 'REFRESH',
            source: this.tabId,
            payload: { token: session.token, refreshToken: session.refreshToken, user },
          });
        }
      }),
      finalize(() => {
        this.pendingRefresh = null;
      }),
      shareReplay({ bufferSize: 1, refCount: false }),
    );
    return this.pendingRefresh;
  }

  /** Mọi endpoint trả LoginResponse (login/google/register) đều dùng chung luồng phiên. */
  private withSession(source: Observable<LoginResponse>): Observable<LoginResponse> {
    return source.pipe(
      tap((response) => {
        this.applyState(response.token, response.refreshToken, response.user);
        this.persist(response.token, response.refreshToken, response.user);
        this.broadcast({
          type: 'LOGIN',
          source: this.tabId,
          payload: { token: response.token, refreshToken: response.refreshToken, user: response.user },
        });
      }),
    );
  }

  logout(): void {
    // Best-effort thu hồi refresh token trên server; state client xóa ngay,
    // ApiAuthService.logout đã nuốt lỗi mạng hộ mình.
    this.api.logout(this.state().refreshToken).subscribe();
    this.applyState(null, null, null);
    this.clearStorage();
    this.broadcast({ type: 'LOGOUT', source: this.tabId });
  }

  /** Message từ tab khác: áp dụng trạng thái, KHÔNG gọi lại API. */
  private onRemoteEvent(event: AuthEvent): void {
    if (!event || event.source === this.tabId) {
      return;
    }
    if (event.type === 'LOGOUT') {
      this.applyState(null, null, null);
      this.clearStorage();
      // Tab phụ bị đăng xuất theo — bắn message cho user biết lý do.
      this.messages.info('REMOTE_LOGOUT');
    } else if ((event.type === 'LOGIN' || event.type === 'REFRESH') && event.payload) {
      this.applyState(event.payload.token, event.payload.refreshToken, event.payload.user);
      this.persist(event.payload.token, event.payload.refreshToken, event.payload.user);
    }
  }

  private restore(): AuthState {
    const storage = safeLocalStorage();
    const token = storage?.getItem(TOKEN_STORAGE_KEY);
    const refreshToken = storage?.getItem(REFRESH_STORAGE_KEY);
    const rawUser = storage?.getItem(USER_STORAGE_KEY);
    if (!token || !rawUser) {
      return { user: null, token: null, refreshToken: null, status: 'unauthenticated' };
    }
    try {
      return { user: JSON.parse(rawUser) as User, token, refreshToken: refreshToken ?? null, status: 'authenticated' };
    } catch {
      return { user: null, token: null, refreshToken: null, status: 'unauthenticated' };
    }
  }

  /** Chỉ đổi cặp token (refresh) — giữ nguyên user hiện tại. */
  private applyTokens(session: SessionTokens): void {
    const user = this.state().user;
    this.applyState(session.token, session.refreshToken, user);
    if (user) {
      this.persist(session.token, session.refreshToken, user);
    }
  }

  private applyState(token: string | null, refreshToken: string | null, user: User | null): void {
    this.state.set({
      token,
      refreshToken,
      user,
      status: token && user ? 'authenticated' : 'unauthenticated',
    });
  }

  private persist(token: string, refreshToken: string, user: User): void {
    safeLocalStorage()?.setItem(TOKEN_STORAGE_KEY, token);
    safeLocalStorage()?.setItem(REFRESH_STORAGE_KEY, refreshToken);
    safeLocalStorage()?.setItem(USER_STORAGE_KEY, JSON.stringify(user));
  }

  private clearStorage(): void {
    safeLocalStorage()?.removeItem(TOKEN_STORAGE_KEY);
    safeLocalStorage()?.removeItem(REFRESH_STORAGE_KEY);
    safeLocalStorage()?.removeItem(USER_STORAGE_KEY);
  }

  private broadcast(event: AuthEvent): void {
    this.channel?.postMessage(event);
  }
}
