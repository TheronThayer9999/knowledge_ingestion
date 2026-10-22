import { Injectable, computed, inject, signal } from '@angular/core';
import { Observable, tap } from 'rxjs';
import { safeLocalStorage } from '../../../shared/browser';
import { AuthState, LoginRequest, LoginResponse, User } from '../models/auth.model';
import { AUTH_CHANNEL_NAME, AuthEvent } from '../models/auth-event.model';
import { AUTH_SERVICE, IAuthService } from './auth.interface';

const TOKEN_STORAGE_KEY = 'ki.auth.token';
const USER_STORAGE_KEY = 'ki.auth.user';

/**
 * Store auth phía client: giữ AuthState trong signal, persist localStorage,
 * và ĐỒNG BỘ LOGIN/LOGOUT GIỮA CÁC TAB qua BroadcastChannel
 * (logout ở tab này → mọi tab khác cũng logout).
 * Gọi backend qua IAuthService (InjectionToken AUTH_SERVICE) — hiện là mock.
 */
@Injectable({ providedIn: 'root' })
export class AuthService {
  private readonly api = inject<IAuthService>(AUTH_SERVICE);
  /** ID riêng của tab này, dùng để lọc message do chính tab gửi ra. */
  private readonly tabId = crypto.randomUUID();
  private readonly channel: BroadcastChannel | null =
    typeof BroadcastChannel !== 'undefined' ? new BroadcastChannel(AUTH_CHANNEL_NAME) : null;

  private readonly state = signal<AuthState>(this.restore());

  readonly user = computed(() => this.state().user);
  readonly token = computed(() => this.state().token);
  readonly isAuthenticated = computed(() => this.state().status === 'authenticated');

  constructor() {
    if (this.channel) {
      this.channel.onmessage = (event: MessageEvent<AuthEvent>) => this.onRemoteEvent(event.data);
    }
  }

  login(request: LoginRequest): Observable<LoginResponse> {
    return this.api.login(request).pipe(
      tap((response) => {
        this.applyState(response.token, response.user);
        this.persist(response.token, response.user);
        this.broadcast({ type: 'LOGIN', source: this.tabId, payload: { token: response.token, user: response.user } });
      }),
    );
  }

  logout(): void {
    // Best-effort báo backend (mock luôn thành công); state client xóa ngay.
    this.api.logout().subscribe();
    this.applyState(null, null);
    this.clearStorage();
    this.broadcast({ type: 'LOGOUT', source: this.tabId });
  }

  /** Message từ tab khác: áp dụng trạng thái, KHÔNG gọi lại API. */
  private onRemoteEvent(event: AuthEvent): void {
    if (!event || event.source === this.tabId) {
      return;
    }
    if (event.type === 'LOGOUT') {
      this.applyState(null, null);
      this.clearStorage();
    } else if (event.type === 'LOGIN' && event.payload) {
      this.applyState(event.payload.token, event.payload.user);
      this.persist(event.payload.token, event.payload.user);
    }
  }

  private restore(): AuthState {
    const storage = safeLocalStorage();
    const token = storage?.getItem(TOKEN_STORAGE_KEY);
    const rawUser = storage?.getItem(USER_STORAGE_KEY);
    if (!token || !rawUser) {
      return { user: null, token: null, status: 'unauthenticated' };
    }
    try {
      return { user: JSON.parse(rawUser) as User, token, status: 'authenticated' };
    } catch {
      return { user: null, token: null, status: 'unauthenticated' };
    }
  }

  private applyState(token: string | null, user: User | null): void {
    this.state.set({
      token,
      user,
      status: token && user ? 'authenticated' : 'unauthenticated',
    });
  }

  private persist(token: string, user: User): void {
    safeLocalStorage()?.setItem(TOKEN_STORAGE_KEY, token);
    safeLocalStorage()?.setItem(USER_STORAGE_KEY, JSON.stringify(user));
  }

  private clearStorage(): void {
    safeLocalStorage()?.removeItem(TOKEN_STORAGE_KEY);
    safeLocalStorage()?.removeItem(USER_STORAGE_KEY);
  }

  private broadcast(event: AuthEvent): void {
    this.channel?.postMessage(event);
  }
}
