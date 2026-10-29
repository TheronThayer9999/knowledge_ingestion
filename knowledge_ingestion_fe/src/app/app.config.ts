import { provideHttpClient, withInterceptors } from '@angular/common/http';
import { ApplicationConfig, provideBrowserGlobalErrorListeners } from '@angular/core';
import { provideRouter } from '@angular/router';

import { routes } from './app.routes';
import { authInterceptor } from './core/auth/auth.interceptor';
import { chatCryptoInterceptor } from './core/crypto/chat-crypto.interceptor';

export const appConfig: ApplicationConfig = {
  providers: [
    provideBrowserGlobalErrorListeners(),
    provideRouter(routes),
    // Thiếu withInterceptors thì mọi request không có Bearer token → 401 toàn bộ.
    // chatCryptoInterceptor đứng SAU auth: token gắn xong mới tới lượt dựng phong
    // bì, và ở chiều về thì nó mở khoá TRƯỚC khi auth nhìn thấy response lỗi.
    provideHttpClient(withInterceptors([authInterceptor, chatCryptoInterceptor])),
  ],
};
