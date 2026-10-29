import { HttpErrorResponse, HttpRequest, HttpResponse } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { ApiService } from '../api/api.service';
import {
  additionalData,
  buildEnvelope,
  DIRECTION,
  fromBase64,
  hkdfInfo,
  hkdfSalt,
  isEnvelope,
  isSessionId,
  NONCE_BYTES,
  SESSION_HEADER,
  SESSION_STALE,
  toBase64,
  type CryptoEnvelope,
  type CryptoSession,
  type Direction,
  type HandshakeDto,
} from './e2ee.models';

/** Độ dài khoá AES-GCM mà backend chốt (256 bit) */
const AES_KEY_BYTES = 32;
/** Xin phiên mới TRƯỚC khi hết hạn ngần này ms — tránh request rơi đúng phút chót */
const RENEW_MARGIN_MS = 60_000;
/** BE không báo hạn thì coi như phiên còn sống 10 phút, đủ cho một lượt hỏi */
const FALLBACK_TTL_MS = 600_000;

/**
 * Khoá phiên E2EE cho nhóm API "khách hỏi AI".
 *
 * Số học mật mã nằm trong thư viện `@noble/*` (audit, không dependency nặng
 * kiểu node-gyp); phần ta viết chỉ là ghép đúng khuôn giao thức mà
 * `src/common/utils/e2ee.go` đã chốt: X25519 → HKDF-SHA256 → AES-256-GCM, AAD
 * binds sid + hướng đi. Thiếu một byte nào là GCM bên server từ chối hết.
 *
 * Khoá CHỈ nằm trong RAM của tab: reload là mất, lượt sau handshake lại — đánh
 * đổi lấy việc không có bản khoá nào rơi xuống localStorage/IndexedDB.
 */
@Injectable({ providedIn: 'root' })
export class ChatCryptoService {
  private readonly api = inject(ApiService);
  private current: CryptoSession | null = null;
  /** Single-flight: mọi request đồng thời chỉ chờ ĐÚNG một lần handshake */
  private pending: Promise<CryptoSession | null> | null = null;
  /** BE trả mode off (hoặc không dựng được khoá) → cả phiên chạy plaintext */
  private disabled = false;

  /** Khoá đang dùng; null = KHÔNG mã hoá — mode optional vẫn chạy bình thường */
  session(): Promise<CryptoSession | null> {
    const live = this.live();
    if (live) {
      return Promise.resolve(live);
    }
    if (this.disabled) {
      return Promise.resolve(null);
    }
    this.pending ??= this.handshake()
      .then((fresh) => {
        this.current = fresh;
        // null TRỞ VỀ TỪ HANDSHAKE = BE nói rõ mode off → cả phiên plaintext.
        this.disabled = fresh === null;
        return fresh;
      })
      // Handshake LỖI (mạng, BE chưa có endpoint) khác với BE tắt mã hoá: request
      // này đi plaintext, lần sau vẫn thử bắt tay lại. Mode required thì BE trả
      // 400 stale và interceptor retry đúng một lần trước khi để lỗi lên UI.
      .catch(() => {
        this.current = null;
        return null;
      })
      .finally(() => {
        this.pending = null;
      });
    return this.pending;
  }

  /** Quên khoá (BE báo stale / tab đổi user) rồi xin phiên mới */
  renew(): Promise<CryptoSession | null> {
    this.current = null;
    return this.session();
  }

  /**
   * Body JSON → phong bì; GET/multipart không có body JSON thì chỉ gắn header
   * sid (response của nó vẫn được server niêm phong — xem middleware Go).
   */
  async sealRequest(
    req: HttpRequest<unknown>,
    session: CryptoSession,
  ): Promise<HttpRequest<unknown>> {
    const setHeaders = { [SESSION_HEADER]: session.sid };
    const payload = jsonPayload(req);
    if (payload === undefined) {
      return req.clone({ setHeaders });
    }
    const body = await this.seal(session, DIRECTION.request, payload);
    return req.clone({ setHeaders, body });
  }

  /** Mở response đã niêm phong; response plaintext (vd abort 400) đi thẳng qua */
  async unsealEvent<T>(event: T, session: CryptoSession): Promise<T> {
    if (!(event instanceof HttpResponse) || !isEnvelope(event.body)) {
      return event;
    }
    return event.clone({ body: await this.open(session, DIRECTION.response, event.body) }) as T;
  }

  /**
   * BE giữ nguyên status khi niêm phong cả response lỗi, nên `error.error` có
   * thể là phong bì. Mở ra cho UI đọc được message tiếng Việt; mở không được
   * thì trả nguyên bản — đừng biến lỗi nghiệp vụ thành lỗi mật mã.
   */
  async normalizeError(error: unknown, session: CryptoSession): Promise<never> {
    if (error instanceof HttpErrorResponse && isEnvelope(error.error)) {
      let plain: unknown;
      try {
        plain = await this.open(session, DIRECTION.response, error.error);
      } catch {
        throw error;
      }
      throw new HttpErrorResponse({
        error: plain,
        headers: error.headers,
        status: error.status,
        statusText: error.statusText,
        url: error.url ?? '',
      });
    }
    throw error;
  }

  /** sid hết hạn/chưa tồn tại — BE trả 400 + header này (CHỦ Ý không phải 401) */
  isStale(error: unknown): boolean {
    return (
      error instanceof HttpErrorResponse &&
      error.status === 400 &&
      error.headers.get(SESSION_HEADER) === SESSION_STALE
    );
  }

  private live(): CryptoSession | null {
    if (this.disabled || !this.current) {
      return null;
    }
    return this.current.expiresAt - Date.now() > RENEW_MARGIN_MS ? this.current : null;
  }

  /** Đổi public key của mình lấy sid + public key của server (body plaintext) */
  private async handshake(): Promise<CryptoSession | null> {
    const { x25519 } = await loadNoble();
    const pair = x25519.keygen();
    const dto = await firstValueFrom(
      this.api.post<HandshakeDto>('/crypto/handshake', { client_public: toBase64(pair.publicKey) }),
    );
    return this.buildSession(dto, pair.secretKey);
  }

  /** HKDF trên shared secret — salt/info khớp từng byte với E2EESaltPrefix/E2EEEInfo */
  private async buildSession(
    dto: HandshakeDto,
    secretKey: Uint8Array,
  ): Promise<CryptoSession | null> {
    const sid = dto.session_id ?? '';
    const peer = dto.server_public ?? '';
    if (dto.mode === 'off' || !isSessionId(sid) || peer === '') {
      return null;
    }
    const { x25519, hkdf, sha256 } = await loadNoble();
    const shared = x25519.getSharedSecret(secretKey, fromBase64(peer));
    const key = hkdf(sha256, shared, hkdfSalt(sid), hkdfInfo(), AES_KEY_BYTES);
    return { sid, key, expiresAt: expiryOf(dto.expires_at) };
  }

  private async seal(
    session: CryptoSession,
    direction: Direction,
    payload: unknown,
  ): Promise<CryptoEnvelope> {
    const { gcm, randomBytes } = await loadNoble();
    const nonce = randomBytes(NONCE_BYTES);
    const ct = gcm(session.key, nonce, additionalData(session.sid, direction)).encrypt(
      bytes(JSON.stringify(payload)),
    );
    return buildEnvelope(session.sid, nonce, ct);
  }

  private async open(
    session: CryptoSession,
    direction: Direction,
    envelope: CryptoEnvelope,
  ): Promise<unknown> {
    const { gcm } = await loadNoble();
    const plain = gcm(
      session.key,
      fromBase64(envelope.nonce),
      additionalData(session.sid, direction),
    ).decrypt(fromBase64(envelope.ct));
    return JSON.parse(new TextDecoder().decode(plain));
  }
}

/** Chỉ body là object JSON mới có phong bì; FormData/Blob/null đi plaintext */ function jsonPayload(
  req: HttpRequest<unknown>,
): unknown {
  const body = req.body;
  if (body === null || typeof body !== 'object') {
    return undefined;
  }
  if (body instanceof FormData || body instanceof Blob || body instanceof ArrayBuffer) {
    return undefined;
  }
  return body;
}

function bytes(text: string): Uint8Array {
  return new TextEncoder().encode(text);
}

/** expires_at RFC3339 từ BE; thiếu hoặc sai khuôn thì coi như còn 10 phút */
function expiryOf(value?: string): number {
  const parsed = value ? Date.parse(value) : Number.NaN;
  return Number.isNaN(parsed) ? Date.now() + FALLBACK_TTL_MS : parsed;
}

/**
 * Số học mật mã nạp TRỄ, có cache.
 *
 * Interceptor này đăng ký trong `app.config` nên nó nằm ở bundle initial; kéo
 * cả `@noble/*` vào bằng import tĩnh là initial vượt ngân sách 600kB của
 * `angular.json` (build ra cảnh báo). Dynamic import đẩy ba gói sang chunk
 * riêng, chỉ tải khi request chat đầu tiên cần dựng phong bì.
 */
interface Noble {
  x25519: typeof import('@noble/curves/ed25519.js').x25519;
  hkdf: typeof import('@noble/hashes/hkdf.js').hkdf;
  sha256: typeof import('@noble/hashes/sha2.js').sha256;
  gcm: typeof import('@noble/ciphers/aes.js').gcm;
  randomBytes: typeof import('@noble/ciphers/utils.js').randomBytes;
}

let noble: Promise<Noble> | null = null;

function loadNoble(): Promise<Noble> {
  noble ??= Promise.all([
    import('@noble/curves/ed25519.js'),
    import('@noble/hashes/hkdf.js'),
    import('@noble/hashes/sha2.js'),
    import('@noble/ciphers/aes.js'),
    import('@noble/ciphers/utils.js'),
  ]).then(([curves, hkdfModule, hashes, ciphers, cipherUtils]) => ({
    x25519: curves.x25519,
    hkdf: hkdfModule.hkdf,
    sha256: hashes.sha256,
    gcm: ciphers.gcm,
    randomBytes: cipherUtils.randomBytes,
  }));
  return noble;
}
