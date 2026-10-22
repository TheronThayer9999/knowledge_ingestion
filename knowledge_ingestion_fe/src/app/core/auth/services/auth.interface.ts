import { InjectionToken } from '@angular/core';
import { Observable } from 'rxjs';
import { LoginRequest, LoginResponse, RegisterRequest } from '../models/auth.model';

/**
 * Hợp đồng với backend auth. AuthService (store phía client) chỉ nói chuyện
 * qua interface này — sau khi có API thật, viết <RealApiAuthService> implements
 * IAuthService rồi đổi provider trong app.config, không sửa gì khác.
 */
export interface IAuthService {
  login(request: LoginRequest): Observable<LoginResponse>;
  /** Đăng nhập qua Google (API thật: exchange code/token; mock: trả user Google). */
  loginWithGoogle(): Observable<LoginResponse>;
  register(request: RegisterRequest): Observable<LoginResponse>;
  logout(): Observable<void>;
}

export const AUTH_SERVICE = new InjectionToken<IAuthService>('AUTH_SERVICE');
