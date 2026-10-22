import { HttpErrorResponse, HttpInterceptorFn } from '@angular/common/http';
import { inject } from '@angular/core';
import { Router } from '@angular/router';
import { catchError, switchMap, throwError } from 'rxjs';
import { MessageService } from '../../shared/feedback/message.service';
import { AuthService } from '../auth/services/auth.service';

/**
 * Đính Bearer JWT vào request API (bỏ qua endpoint auth/users — chính nó
 * phát token, đính làm gì). Gặp 401: thử refresh rotation ĐÚNG MỘT LẦN
 * (AuthService dedupe request song song), retry request bằng token mới;
 * refresh luôn fail → phiên chết thật → message + logout + về login.
 */
export const jwtInterceptor: HttpInterceptorFn = (req, next) => {
  const auth = inject(AuthService);
  const router = inject(Router);
  const messages = inject(MessageService);
  const token = auth.token();

  const isPublicEndpoint = req.url.includes('/auth/') || req.url.includes('/users');
  if (!token || isPublicEndpoint) {
    return next(req);
  }

  const attach = (jwt: string) => req.clone({ setHeaders: { Authorization: `Bearer ${jwt}` } });

  return next(attach(token)).pipe(
    catchError((err: unknown) => {
      if (!(err instanceof HttpErrorResponse) || err.status !== 401) {
        return throwError(() => err);
      }
      return auth.refreshSession().pipe(
        switchMap((session) => next(attach(session.token))),
        catchError(() => {
          // Refresh fail (token hết hạn/thu hồi/reuse) — dọn phiên, báo user.
          messages.error('SESSION_EXPIRED');
          auth.logout();
          void router.navigate(['/login'], { queryParams: { returnUrl: router.url } });
          return throwError(() => err);
        }),
      );
    }),
  );
};
