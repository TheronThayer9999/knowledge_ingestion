import { inject } from '@angular/core';
import { type CanActivateFn, Router } from '@angular/router';
import { MessageService } from '../../shared/feedback/message.service';
import { AuthService } from './auth.service';

/** Chưa đăng nhập → /login kèm redirect để quay lại đúng trang đang mở */
export const authGuard: CanActivateFn = (_route, state) => {
  const auth = inject(AuthService);
  const router = inject(Router);
  return auth.isAuthenticated()
    ? true
    : router.createUrlTree(['/login'], { queryParams: { redirect: state.url } });
};

/** Đã đăng nhập thì không cho vào /login, /register nữa */
export const guestGuard: CanActivateFn = () => {
  const auth = inject(AuthService);
  const router = inject(Router);
  return auth.isAuthenticated() ? router.createUrlTree(['/']) : true;
};

/** Trang admin (role >= 1) — thiếu quyền thì báo toast và về trang chủ */
export const adminGuard: CanActivateFn = () => {
  const auth = inject(AuthService);
  const router = inject(Router);
  const messages = inject(MessageService);
  if (!auth.isAuthenticated()) {
    return router.createUrlTree(['/login']);
  }
  if (!auth.isAdmin()) {
    messages.error('AUTH_ERROR_FORBIDDEN');
    return router.createUrlTree(['/']);
  }
  return true;
};
