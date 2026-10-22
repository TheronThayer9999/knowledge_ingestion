import { HttpErrorResponse, HttpInterceptorFn } from '@angular/common/http';
import { inject } from '@angular/core';
import { Router } from '@angular/router';
import { catchError, throwError } from 'rxjs';
import { AuthService } from '../auth/services/auth.service';

/** Đính Bearer JWT vào mọi request (trừ các endpoint auth), và logout khi gặp 401. */
export const jwtInterceptor: HttpInterceptorFn = (req, next) => {
  const auth = inject(AuthService);
  const router = inject(Router);
  const token = auth.token();

  const isAuthEndpoint = req.url.includes('/login') || req.url.includes('/logout');
  if (!token || isAuthEndpoint) {
    return next(req);
  }

  const request = req.clone({ setHeaders: { Authorization: `Bearer ${token}` } });

  return next(request).pipe(
    catchError((err: unknown) => {
      if (err instanceof HttpErrorResponse && err.status === 401) {
        // Token hết hạn/bị thu hồi — dọn state (đồng bộ cả tab qua BroadcastChannel) rồi về login.
        auth.logout();
        void router.navigate(['/login'], { queryParams: { returnUrl: router.url } });
      }
      return throwError(() => err);
    }),
  );
};
