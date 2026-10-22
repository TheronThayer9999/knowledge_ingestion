/** Model domain của auth — khớp contract backend Go (dtos.UserResponse, snake_case JSON). */

/** Người dùng — map từ dtos.UserResponse. */
export interface User {
  id: number;
  username: string;
  email: string;
  phone: string;
  avatar: string;
  active: boolean;
  /** 0 = user thường, 1 = admin (domain.RoleAdmin bên Go). */
  role: number;
  /** ISO string, backend có thể omit nếu chưa đăng nhập lần nào. */
  lastLogin?: string;
}

/** Body gửi lên endpoint login. */
export interface LoginRequest {
  username: string;
  password: string;
}

/** Body gửi lên endpoint tạo tài khoản (POST /users — password tối thiểu 8 ký tự). */
export interface RegisterRequest {
  username: string;
  email: string;
  password: string;
}

/** Cặp token từ login/refresh — refresh token rotation, phải lưu bản mới nhất. */
export interface SessionTokens {
  token: string;
  expiresAt: string;
  refreshToken: string;
  refreshExpiresAt: string;
}

/** Response đăng nhập thành công = cặp token + user. */
export interface LoginResponse extends SessionTokens {
  user: User;
}

export type AuthStatus = 'authenticated' | 'unauthenticated';

/** Trạng thái auth giữ trong store (signal) của AuthService. */
export interface AuthState {
  user: User | null;
  token: string | null;
  refreshToken: string | null;
  status: AuthStatus;
}
