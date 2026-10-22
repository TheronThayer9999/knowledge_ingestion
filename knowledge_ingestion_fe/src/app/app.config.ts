import { ApplicationConfig, provideBrowserGlobalErrorListeners, provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withInterceptors } from '@angular/common/http';
import { provideAnimationsAsync } from '@angular/platform-browser/animations/async';
import { provideRouter, withComponentInputBinding } from '@angular/router';

import { routes } from './app.routes';
import { ApiAuthService } from './core/auth/services/api-auth.service';
import { API_BASE_URL, AUTH_SERVICE } from './core/auth/services/auth.interface';
import { jwtInterceptor } from './core/interceptors/jwt.interceptor';

export const appConfig: ApplicationConfig = {
  providers: [
    provideBrowserGlobalErrorListeners(),
    provideZonelessChangeDetection(),
    provideRouter(routes, withComponentInputBinding()),
    provideAnimationsAsync(),
    provideHttpClient(withInterceptors([jwtInterceptor])),
    // Auth nối API thật (cmd/apis chạy port 9001, CORS đã allow 4200).
    // Offline/dev không cần backend: đổi useClass về MockAuthService.
    { provide: API_BASE_URL, useValue: 'http://localhost:9001/api/v1' },
    { provide: AUTH_SERVICE, useClass: ApiAuthService },
  ],
};
