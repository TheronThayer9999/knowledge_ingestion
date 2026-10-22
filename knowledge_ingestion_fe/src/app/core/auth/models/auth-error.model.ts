/**
 * Mã lỗi auth — component map sang key i18n `login.errors.<CODE>`,
 * không hard-code chuỗi người dùng trong service.
 */
export type AuthErrorCode = 'INVALID_CREDENTIALS' | 'USERNAME_TAKEN' | 'GENERIC';

export class AuthApiError extends Error {
  constructor(public readonly code: AuthErrorCode) {
    super(code);
    this.name = 'AuthApiError';
  }
}
