import { HttpEvent, HttpHandlerFn, HttpInterceptorFn, HttpRequest } from '@angular/common/http';
import { inject } from '@angular/core';
import { catchError, from, mergeMap, Observable, switchMap, throwError } from 'rxjs';
import { ChatCryptoService } from './chat-crypto.service';
import { inScope, type CryptoSession } from './e2ee.models';

/**
 * Lớp mã hoá payload chat cho MỌI request trong phạm vi (xem `inScope`):
 *
 * - body JSON → phong bì `{v,sid,nonce,ct}` + header `X-KI-Session`;
 * - GET/multipart không body JSON → chỉ gắn header, server vẫn niêm phong response;
 * - response (kể cả response lỗi, giữ nguyên status) được mở trước khi xuống UI;
 * - 400 + `X-KI-Session: stale` → handshake lại và gửi ĐÚNG một lần.
 *
 * Đứng SAU authInterceptor trong `withInterceptors`, nên token đã gắn xong và
 * phía đọc response thì khoá được mở trước khi auth nhìn thấy lỗi.
 */
export const chatCryptoInterceptor: HttpInterceptorFn = (req, next) => {
  const crypto = inject(ChatCryptoService);
  if (!inScope(req.url)) {
    return next(req);
  }
  return from(crypto.session()).pipe(
    switchMap((session) => (session ? round(req, next, crypto, session, false) : next(req))),
  );
};

/**
 * Một lượt đi–về đã mã hoá. `retried` là cờ chống lặp: BE đá sid hết hạn thì xin
 * phiên mới rồi thử lại ĐÚNG một lần, không có chuỗi retry vô hạn.
 */
function round(
  req: HttpRequest<unknown>,
  next: HttpHandlerFn,
  crypto: ChatCryptoService,
  session: CryptoSession,
  retried: boolean,
): Observable<HttpEvent<unknown>> {
  // sealRequest là async vì số học noble nạp trễ (giữ bundle initial nhẹ)
  return from(crypto.sealRequest(req, session)).pipe(
    switchMap((sent) =>
      next(sent).pipe(
        mergeMap((event) => from(crypto.unsealEvent(event, session))),
        catchError((error: unknown) => {
          if (crypto.isStale(error) && !retried) {
            return from(crypto.renew()).pipe(
              switchMap((fresh) =>
                fresh ? round(req, next, crypto, fresh, true) : throwError(() => error),
              ),
            );
          }
          return from(crypto.normalizeError(error, session));
        }),
      ),
    ),
  );
}
