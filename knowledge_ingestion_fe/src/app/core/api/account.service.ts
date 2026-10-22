import { inject, Injectable } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable } from 'rxjs';
import { User } from '../auth/models/auth.model';
import { API_BASE_URL } from '../auth/services/auth.interface';
import { unwrap, unwrapVoid } from './api.helpers';
import { ApiResponse, ChangePasswordRequest, LogoutResponse } from './api.models';

/** Tài khoản của chính mình: profile, đổi mật khẩu, thu hồi mọi phiên. */
@Injectable({ providedIn: 'root' })
export class AccountService {
  private readonly http = inject(HttpClient);
  private readonly base = inject(API_BASE_URL);

  profile(): Observable<User> {
    return this.http.get<ApiResponse<User>>(`${this.base}/auth/profile`).pipe(unwrap());
  }

  /** BE trả data = null khi thành công — dùng unwrapVoid, bỏ qua data. */
  changePassword(req: ChangePasswordRequest): Observable<void> {
    return this.http
      .post<ApiResponse<unknown>>(`${this.base}/auth/change-password`, req)
      .pipe(unwrapVoid());
  }

  logoutAll(): Observable<LogoutResponse> {
    return this.http.post<ApiResponse<LogoutResponse>>(`${this.base}/auth/logout-all`, {}).pipe(unwrap());
  }
}
