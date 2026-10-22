import { InjectionToken } from '@angular/core';
import { Observable } from 'rxjs';
import { LoginRequest, LoginResponse, RegisterRequest, SessionTokens } from '../models/auth.model';

/**
 * Hợp đồng với backend auth. AuthService (store phía client) chỉ nói chuyện
 * qua interface này — đổi mock ↔ API thật chỉ thay provider trong app.config.
 * Lỗi trả về luôn là AuthApiError (mã nghiệp vụ), component tự map sang i18n.
 */
export interface IAuthService {
  login(request: LoginRequest): Observable<LoginResponse>;
  /** Đăng nhập qua Google — backend chưa có OAuth, ApiAuthService đang mock tạm. */
  loginWithGoogle(): Observable<LoginResponse>;
  /** Tạo tài khoản (POST /users) — backend Go không tự đăng nhập nên impl phải login lại. */
  register(request: RegisterRequest): Observable<LoginResponse>;
  /** Đổi refresh token lấy cặp token mới (rotation — token cũ bị thu hồi ngay). */
  refresh(refreshToken: string): Observable<SessionTokens>;
  /** Thu hồi refresh token trên server (idempotent — lỗi mạng không được chặn logout client). */
  logout(refreshToken: string | null): Observable<void>;
}

/** Gốc API backend — provide trong app.config (dev: http://localhost:9001/api/v1). */
export const API_BASE_URL = new InjectionToken<string>('API_BASE_URL');

export const AUTH_SERVICE = new InjectionToken<IAuthService>('AUTH_SERVICE');
