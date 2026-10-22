/**
 * Mã lỗi auth — component map sang key i18n, không hard-code chuỗi người
 * dùng trong service. ApiAuthService map HTTP status của backendGo về đây:
 * 401 → INVALID_CREDENTIALS, 403 → ACCOUNT_LOCKED, 409 → USERNAME_TAKEN.
 */
export type AuthErrorCode = 'INVALID_CREDENTIALS' | 'ACCOUNT_LOCKED' | 'USERNAME_TAKEN' | 'GENERIC';

export class AuthApiError extends Error {
  constructor(public readonly code: AuthErrorCode) {
    super(code);
    this.name = 'AuthApiError';
  }
}
