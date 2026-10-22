/**
 * Helpers dùng chung mọi API service: unwrap envelope {status, code, message, data}
 * và map HttpErrorResponse -> ApiRequestError (message gốc của server để toast raw).
 */
import { HttpErrorResponse } from '@angular/common/http';
import { Observable, catchError, map, throwError } from 'rxjs';
import { ApiResponse } from './api.models';

/** Lỗi API có code HTTP + message gốc tiếng Việt từ server (dtos.ResponseResource). */
export class ApiRequestError extends Error {
  constructor(
    readonly code: number,
    message: string,
  ) {
    super(message);
    this.name = 'ApiRequestError';
  }
}

/** Math catch chung của mọi service: HttpErrorResponse -> ApiRequestError. */
export function toApiError(err: unknown): Observable<never> {
  if (err instanceof HttpErrorResponse) {
    const body = err.error as ApiResponse<unknown> | null;
    return throwError(() => new ApiRequestError(err.status, body?.message || err.statusText));
  }
  return throwError(() => err);
}

/** Pipe unwrap envelope sau mọi call — data null hoặc status false là lỗi. */
export function unwrap<T>(): (source: Observable<ApiResponse<T>>) => Observable<T> {
  return (source) =>
    source.pipe(
      map((res) => {
        if (!res.status || res.data === null || res.data === undefined) {
          throw new ApiRequestError(res.code, res.message);
        }
        return res.data;
      }),
      catchError(toApiError),
    );
}

/** Cho thao tác thành công nhưng data = null (đổi mật khẩu...) — chỉ cần status đúng. */
export function unwrapVoid<T = unknown>(): (source: Observable<ApiResponse<T>>) => Observable<void> {
  return (source) =>
    source.pipe(
      map((res) => {
        if (!res.status) {
          throw new ApiRequestError(res.code, res.message);
        }
      }),
      catchError(toApiError),
    );
}
