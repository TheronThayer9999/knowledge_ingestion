/** Model domain của auth — tách khỏi component, dùng chung cho core/features. */

/** Người dùng sau khi đăng nhập thành công. */
export interface User {
  id: string;
  username: string;
  displayName: string;
  email: string;
  roles: string[];
}

/** Body gửi lên endpoint login. */
export interface LoginRequest {
  username: string;
  password: string;
}

/** Response trả về từ endpoint login. */
export interface LoginResponse {
  /** JWT access token (mock: chuỗi JWT giả lập). */
  token: string;
  user: User;
}

export type AuthStatus = 'authenticated' | 'unauthenticated';

/** Trạng thái auth giữ trong store (signal) của AuthService. */
export interface AuthState {
  user: User | null;
  token: string | null;
  status: AuthStatus;
}
