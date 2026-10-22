import { inject, Injectable } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable } from 'rxjs';
import { API_BASE_URL } from '../auth/services/auth.interface';
import { unwrap } from './api.helpers';
import { ApiResponse, UserListResponse } from './api.models';

/** Danh sách người dùng (GET /users — mọi user đã đăng nhập đọc được). */
@Injectable({ providedIn: 'root' })
export class UsersService {
  private readonly http = inject(HttpClient);
  private readonly base = inject(API_BASE_URL);

  list(): Observable<UserListResponse> {
    return this.http.get<ApiResponse<UserListResponse>>(`${this.base}/users`).pipe(unwrap());
  }
}
