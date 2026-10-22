import { User } from './auth.model';

/**
 * Message trao đổi qua BroadcastChannel `ki-auth` để đồng bộ login/logout
 * giữa các tab: logout ở tab này thì tab khác cũng logout theo.
 */
export type AuthEventType = 'LOGIN' | 'LOGOUT';

export interface AuthEvent {
  type: AuthEventType;
  /** ID của tab phát sinh sự kiện — tab nhận bỏ qua message do chính nó gửi. */
  source: string;
  /** Với LOGIN: token + user để các tab khác khôi phục phiên. LOGOUT: không có. */
  payload?: {
    token: string;
    user: User;
  };
}

export const AUTH_CHANNEL_NAME = 'ki-auth';
