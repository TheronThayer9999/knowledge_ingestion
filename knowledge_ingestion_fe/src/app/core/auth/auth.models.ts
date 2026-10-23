export const ROLE_USER = 0;
export const ROLE_ADMIN = 1;

/** User đã map sang camelCase để dùng trong UI */
export interface AuthUser {
  id: number;
  username: string;
  email: string;
  phone: string;
  avatar: string;
  active: boolean;
  role: number;
  lastLogin: string | null;
}

export interface LoginCredentials {
  username: string;
  password: string;
}

/** Cặp token + hạn — luôn thay CẢ HAI khi refresh (refresh token dùng 1 lần) */
export interface TokenPair {
  token: string;
  expiresAt: string;
  refreshToken: string;
  refreshExpiresAt: string;
}

/** Phiên lưu trong localStorage['ki.auth'] — có user để render trước khi gọi API */
export interface StoredSession extends TokenPair {
  user: AuthUser;
}

export type AuthStatus = 'unknown' | 'authenticated' | 'anonymous';

/** Message đồng bộ giữa các tab qua BroadcastChannel('ki-auth') */
export type AuthBroadcast = { type: 'LOGIN'; session: StoredSession } | { type: 'LOGOUT' };

// ---- DTO khớp JSON backend (snake_case) ----

export interface UserDto {
  id: number;
  username: string;
  email: string;
  phone: string;
  avatar: string;
  active: boolean;
  role: number;
  last_login?: string | null;
}

export interface LoginResponseDto {
  token: string;
  expires_at: string;
  refresh_token: string;
  refresh_expires_at: string;
  user: UserDto;
}

export interface RefreshResponseDto {
  token: string;
  expires_at: string;
  refresh_token: string;
  refresh_expires_at: string;
}

export function toAuthUser(dto: UserDto): AuthUser {
  return {
    id: dto.id,
    username: dto.username,
    email: dto.email,
    phone: dto.phone,
    avatar: dto.avatar,
    active: dto.active,
    role: dto.role,
    lastLogin: dto.last_login ?? null,
  };
}

export function toStoredSession(dto: LoginResponseDto): StoredSession {
  return {
    token: dto.token,
    expiresAt: dto.expires_at,
    refreshToken: dto.refresh_token,
    refreshExpiresAt: dto.refresh_expires_at,
    user: toAuthUser(dto.user),
  };
}
