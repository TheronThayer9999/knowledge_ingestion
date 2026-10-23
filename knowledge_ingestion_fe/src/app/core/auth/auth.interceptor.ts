import { HttpErrorResponse, HttpInterceptorFn } from '@angular/common/http';
import { inject } from '@angular/core';
import { Router } from '@angular/router';
import { catchError, from, switchMap, throwError } from 'rxjs';
import { ApiError } from '../api/api.models';
import { MessageService } from '../../shared/feedback/message.service';
import { AuthService } from './auth.service';

/** login/refresh tự xử lý 401 của chính nó — không refresh vòng lặp */
const SKIP_REFRESH_PATHS = ['/auth/login', '/auth/refresh'];

/**
 * Gắn Bearer token + xử lý 401:
 * gọi AuthService.refresh() (single-flight) rồi retry đúng 1 lần.
 * Refresh thất bại → xoá phiên (AuthService đã broadcast LOGOUT), báo
 * "phiên hết hạn" và đưa về /login kèm redirect.
 */
export const authInterceptor: HttpInterceptorFn = (req, next) => {
  const auth = inject(AuthService);
  const router = inject(Router);
  const messages = inject(MessageService);

  if (SKIP_REFRESH_PATHS.some((path) => req.url.includes(path))) {
    return next(req);
  }

  const session = auth.session();
  const authorized = session
    ? req.clone({ setHeaders: { Authorization: `Bearer ${session.token}` } })
    : req;

  return next(authorized).pipe(
    catchError((error: unknown) => {
      if (!(error instanceof HttpErrorResponse) || error.status !== 401) {
        return throwError(() => error);
      }

      return from(auth.refresh()).pipe(
        switchMap((fresh) =>
          next(req.clone({ setHeaders: { Authorization: `Bearer ${fresh.token}` } })),
        ),
        catchError((refreshError: unknown) => {
          messages.error('AUTH_SESSION_EXPIRED');
          const redirect = router.url !== '/login' ? router.url : undefined;
          void router.navigate(['/login'], {
            queryParams: redirect ? { redirect } : undefined,
          });
          return throwError(() =>
            refreshError instanceof Error ? refreshError : new ApiError(401, 401, ''),
          );
        }),
      );
    }),
  );
};
