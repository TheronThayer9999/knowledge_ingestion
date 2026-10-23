import { HttpClient, HttpErrorResponse, HttpParams } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { catchError, map, Observable, throwError } from 'rxjs';
import { environment } from '../../../environments/environment';
import { ApiEnvelope, ApiError } from './api.models';

type QueryValue = string | number | boolean;

interface RequestOptions {
  body?: unknown;
  params?: HttpParams;
}

/**
 * Client HTTP mỏng cho backend Go: gắn base URL, bóc envelope
 * `{status, code, message, data}` và ném `ApiError` đồng nhất cho UI.
 * Mọi service miền (auth, article…) gọi qua đây, không dùng HttpClient trực tiếp.
 */
@Injectable({ providedIn: 'root' })
export class ApiService {
  private readonly http = inject(HttpClient);

  get<T>(path: string, query?: Record<string, QueryValue>): Observable<T> {
    return this.request<T>('GET', path, { params: toParams(query) });
  }

  post<T>(path: string, body?: unknown): Observable<T> {
    return this.request<T>('POST', path, { body });
  }

  put<T>(path: string, body?: unknown): Observable<T> {
    return this.request<T>('PUT', path, { body });
  }

  delete<T>(path: string): Observable<T> {
    return this.request<T>('DELETE', path);
  }

  private request<T>(method: string, path: string, options: RequestOptions = {}): Observable<T> {
    return this.http
      .request<ApiEnvelope<T>>(method, `${environment.apiBaseUrl}${path}`, options)
      .pipe(
        map((envelope) => {
          if (!envelope.status || envelope.code !== 0) {
            throw new ApiError(200, envelope.code, envelope.message);
          }
          return envelope.data;
        }),
        catchError((error: unknown) => throwError(() => toApiError(error))),
      );
  }
}

function toParams(query?: Record<string, QueryValue>): HttpParams | undefined {
  if (!query) {
    return undefined;
  }
  let params = new HttpParams();
  for (const [key, value] of Object.entries(query)) {
    params = params.set(key, String(value));
  }
  return params;
}

/** Chuyển mọi lỗi transport về ApiError; giữ nguyên ApiError đã có */
function toApiError(error: unknown): ApiError {
  if (error instanceof ApiError) {
    return error;
  }
  if (error instanceof HttpErrorResponse) {
    // Server lỗi luôn trả envelope trong body — ưu tiên message nghiệp vụ
    const body = error.error as Partial<ApiEnvelope<unknown>> | null;
    const code = typeof body?.code === 'number' ? body.code : error.status;
    const message = typeof body?.message === 'string' ? body.message : error.message;
    return new ApiError(error.status, code, message);
  }
  return new ApiError(0, 0, '');
}
