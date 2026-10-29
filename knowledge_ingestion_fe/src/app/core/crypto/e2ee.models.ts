/**
 * Khuôn mã hoá payload chat (Đợt 29) — bản JS của `src/common/utils/e2ee.go`.
 * Đổi MỘT chữ ở đây mà không đổi bên Go là mọi request vỡ (GCM kiểm AAD/salt
 * từng byte), nên các hằng số dưới là giao thức, không phải tùy thích.
 *
 * File này THUẦN: không chạm `crypto.subtle`, không chạm DOM — nhờ vậy test
 * chạy được trong node mà không cần trình duyệt.
 */

/** Header mang id phiên — BE đọc cái này để tìm khoá, FE đọc lại khi bị stale */
export const SESSION_HEADER = 'X-KI-Session';
/** BE gắn header này vào response khi sid đã hết hạn/chưa từng tồn tại */
export const SESSION_STALE = 'stale';

/** Phiên bản phong bì — BE từ chối mọi giá trị khác 1 */
export const ENVELOPE_VERSION = 1;
/** Tiền tố AAD/salt: ràng buộc khoá với đúng phiên + đúng hướng đi */
export const SALT_PREFIX = 'ki-e2ee-v1|';
/** Tham số `info` của HKDF — phải khớp E2EEInfo bên Go */
export const HKDF_INFO = 'ki-api-aes-256-gcm';
/** Hướng của một khối dữ liệu qua lại */
export const DIRECTION = { request: 'req', response: 'rsp' } as const;

/** Độ dài sid BE sinh ra (32 ký tự hex) */
export const SESSION_ID_LENGTH = 32;
/** Nonce AES-GCM luôn 12 byte */
export const NONCE_BYTES = 12;

/** Hướng của một lượt mã hoá */
export type Direction = (typeof DIRECTION)[keyof typeof DIRECTION];

/** Phong bì trên dây: 4 field này là tất cả những gì BE chịu đọc */
export interface CryptoEnvelope {
  v: number;
  sid: string;
  nonce: string;
  ct: string;
}

/** Trả lời của POST /crypto/handshake (đã bóc envelope của API) */
export interface HandshakeDto {
  session_id?: string;
  server_public?: string;
  expires_at?: string;
  mode?: string;
}

/** Khoá phiên đang dùng — chỉ sống trong RAM của tab này, không xuống storage */
export interface CryptoSession {
  sid: string;
  /** Khoá AES-256 đã derive (32 byte), vừa đủ cho AES-GCM 256 */
  key: Uint8Array;
  /** Epoch ms hết hạn BE báo — FE dùng để handshake lại TRƯỚC khi BE đá */
  expiresAt: number;
}

/**
 * Phạm vi mã hoá = đúng nhóm "khách hỏi AI": /chat (kể cả /chat/attachments)
 * và /conversations*. Khớp từng ấy với `inScope()` bên middleware Go — login,
 * admin, /llm/* vẫn plaintext. /crypto/handshake deliberately KHÔNG nằm trong
 * phạm vi: nó là thứ dựng nên khoá, không thể mã hoá bằng chính khoá đó.
 */
export function inScope(url: string): boolean {
  if (isHandshake(url)) {
    return false;
  }
  const path = apiPath(url);
  return path === '/chat' || path.startsWith('/chat/') || path.startsWith('/conversations');
}

/** true với ĐÚNG path của handshake — nó dựng khoá nên không thể tự mã hoá */
export function isHandshake(url: string): boolean {
  return apiPath(url) === '/crypto/handshake';
}

/** Bỏ origin + prefix /api/v1, trả path tương đối như BE thấy */
function apiPath(url: string): string {
  const withoutOrigin = url.replace(/^https?:\/\/[^/]+/i, '').split('?')[0];
  return withoutOrigin.replace(/^.*\/api\/v1/, '') || '/';
}

/** Nhận diện phong bì từ một object đã parse (KHÔNG tin field thừa) */
export function isEnvelope(value: unknown): value is CryptoEnvelope {
  if (typeof value !== 'object' || value === null) {
    return false;
  }
  const obj = value as Record<string, unknown>;
  return (
    obj['v'] === ENVELOPE_VERSION &&
    typeof obj['sid'] === 'string' &&
    typeof obj['nonce'] === 'string' &&
    typeof obj['ct'] === 'string'
  );
}

/** sid hợp lệ hình thức: 32 ký tự hex thường (BE còn soi DB nữa) */
export function isSessionId(value: unknown): value is string {
  return typeof value === 'string' && new RegExp(`^[0-9a-f]{${SESSION_ID_LENGTH}}$`).test(value);
}

/** AAD = tiền tố|sid|hướng — khoá chặt bản mã vào đúng chiều đi */
export function additionalData(sid: string, direction: Direction): Uint8Array {
  return utf8(`${SALT_PREFIX}${sid}|${direction}`);
}

/** salt HKDF = tiền tố|sid — mỗi sid một khoá, không dùng lại */
export function hkdfSalt(sid: string): Uint8Array {
  return utf8(`${SALT_PREFIX}${sid}`);
}

/** info HKDF — chuỗi cố định, xuất ra byte để khớp đúng E2EEEInfo bên Go */
export function hkdfInfo(): Uint8Array {
  return utf8(HKDF_INFO);
}

function utf8(text: string): Uint8Array {
  return new TextEncoder().encode(text);
}

/** base64 chuẩn (có padding) — BE decode bằng StdEncoding, vẫn đọc được bản cắt padding */
export function toBase64(bytes: Uint8Array): string {
  let binary = '';
  bytes.forEach((byte) => {
    binary += String.fromCharCode(byte);
  });
  return btoa(binary);
}

/** base64 → byte; chấp nhận cả bản bị cắt padding */
export function fromBase64(text: string): Uint8Array {
  const padded = text + '='.repeat((4 - (text.length % 4)) % 4);
  const binary = atob(padded);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}

/** Dựng phong bì đã mã hoá cho một chuỗi JSON plaintext */
export function buildEnvelope(sid: string, nonce: Uint8Array, cipher: Uint8Array): CryptoEnvelope {
  return {
    v: ENVELOPE_VERSION,
    sid,
    nonce: toBase64(nonce),
    ct: toBase64(cipher),
  };
}
