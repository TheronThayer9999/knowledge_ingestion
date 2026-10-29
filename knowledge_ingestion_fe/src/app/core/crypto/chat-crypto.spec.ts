import { HttpClient, provideHttpClient, withInterceptors } from '@angular/common/http';
import {
  HttpTestingController,
  provideHttpClientTesting,
  type TestRequest,
} from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { gcm } from '@noble/ciphers/aes.js';
import { randomBytes } from '@noble/ciphers/utils.js';
import { x25519 } from '@noble/curves/ed25519.js';
import { hkdf } from '@noble/hashes/hkdf.js';
import { sha256 } from '@noble/hashes/sha2.js';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { environment } from '../../../environments/environment';
import { chatCryptoInterceptor } from './chat-crypto.interceptor';
import {
  additionalData,
  buildEnvelope,
  DIRECTION,
  fromBase64,
  hkdfInfo,
  hkdfSalt,
  inScope,
  isEnvelope,
  isSessionId,
  NONCE_BYTES,
  SESSION_HEADER,
  toBase64,
  type CryptoEnvelope,
  type Direction,
  type HandshakeDto,
} from './e2ee.models';

/**
 * Spec của CẢ ba mảnh giao thức (models + service + interceptor) trong một file:
 * phía server giả cần đúng bộ số học mà `src/common/utils/e2ee.go` mô tả, và nó
 * dùng chung cho mọi ca ở dưới — tách file là chép lại nguyên khối này.
 *
 * Không mock mật mã: `@noble/*` chạy được trong node nên test đi hết đường đi
 * thật (ECDH → HKDF → AES-GCM), chỉ HTTP là giả lập.
 */

const api = (path: string): string => `${environment.apiBaseUrl}${path}`;
const tick = (): Promise<void> => new Promise((resolve) => setTimeout(resolve, 0));
/** Body chuẩn của API Go: handshake đi qua ApiService nên phải có envelope {status,…} */
const ok = (data: unknown): object => ({ status: true, code: 0, message: 'success', data });

/** Server giả: dựng khoá từ chính public key client gửi lên, niêm phong lại như BE Go */
class FakeCryptoServer {
  readonly sid = 'ab'.repeat(16);
  private readonly secret = x25519.utils.randomSecretKey();
  private readonly publicKey = x25519.getPublicKey(this.secret);
  private key: Uint8Array | undefined;

  handshake(clientPublic: string): HandshakeDto {
    const shared = x25519.getSharedSecret(this.secret, fromBase64(clientPublic));
    this.key = hkdf(sha256, shared, hkdfSalt(this.sid), hkdfInfo(), 32);
    return {
      session_id: this.sid,
      server_public: toBase64(this.publicKey),
      mode: 'optional',
      expires_at: new Date(Date.now() + 600_000).toISOString(),
    };
  }

  /** Mở phong bì request → plaintext; sai hướng hoặc sai sid là GCM chết ngay */
  openRequest(envelope: CryptoEnvelope): unknown {
    return JSON.parse(this.plain(envelope, DIRECTION.request));
  }

  /** Dựng response đã niêm phong cho một payload bất kỳ */
  sealResponse(plain: unknown): CryptoEnvelope {
    const nonce = randomBytes(NONCE_BYTES);
    const ct = this.aead(nonce, DIRECTION.response).encrypt(
      new TextEncoder().encode(JSON.stringify(plain)),
    );
    return buildEnvelope(this.sid, nonce, ct);
  }

  private plain(envelope: CryptoEnvelope, direction: Direction): string {
    return new TextDecoder().decode(
      this.aead(fromBase64(envelope.nonce), direction).decrypt(fromBase64(envelope.ct)),
    );
  }

  private aead(nonce: Uint8Array, direction: Direction): ReturnType<typeof gcm> {
    if (!this.key) {
      throw new Error('server giả chưa handshake');
    }
    return gcm(this.key, nonce, additionalData(this.sid, direction));
  }
}

describe('e2ee.models', () => {
  it('khoá đúng nhóm route khách hỏi AI', () => {
    const scoped = [
      api('/chat'),
      api('/chat/attachments'),
      api('/conversations'),
      api('/conversations/12/messages'),
    ];
    scoped.forEach((url) => expect(inScope(url)).toBe(true));
  });

  it('để nguyên login, admin, llm và handshake', () => {
    const plain = [
      api('/auth/login'),
      api('/crypto/handshake'),
      api('/llm/options'),
      api('/users'),
      api('/admin/queue'),
    ];
    plain.forEach((url) => expect(inScope(url)).toBe(false));
  });

  it('nối đúng chuỗi AAD/salt mà bên Go chốt', () => {
    const decode = (bytes: Uint8Array): string => new TextDecoder().decode(bytes);
    expect(decode(additionalData('aa'.repeat(8), DIRECTION.request))).toBe(
      'ki-e2ee-v1|' + 'aa'.repeat(8) + '|req',
    );
    expect(decode(additionalData('x', DIRECTION.response))).toBe('ki-e2ee-v1|x|rsp');
    expect(decode(hkdfSalt('x'))).toBe('ki-e2ee-v1|x');
    expect(decode(hkdfInfo())).toBe('ki-api-aes-256-gcm');
  });

  it('nhận diện phong bì chặt: thiếu field hoặc sai version là không phải', () => {
    const good = { v: 1, sid: 'a', nonce: 'b', ct: 'c' };
    expect(isEnvelope(good)).toBe(true);
    expect(isEnvelope({ ...good, v: 2 })).toBe(false);
    expect(isEnvelope({ sid: 'a', nonce: 'b', ct: 'c' })).toBe(false);
    expect(isEnvelope(null)).toBe(false);
    expect(isEnvelope({ status: true, code: 0 })).toBe(false);
  });

  it('sid chỉ chấp nhận 32 hex thường', () => {
    expect(isSessionId('ab'.repeat(16))).toBe(true);
    expect(isSessionId('AB'.repeat(16))).toBe(false);
    expect(isSessionId('zz'.repeat(16))).toBe(false);
    expect(isSessionId('ab')).toBe(false);
  });

  it('base64 đi được cả bản có và không padding', () => {
    const raw = new Uint8Array([0, 1, 2, 253, 254, 255]);
    const padded = toBase64(raw);
    expect(fromBase64(padded)).toEqual(raw);
    expect(fromBase64(padded.replace(/=+$/, ''))).toEqual(raw);
  });
});

describe('chatCryptoInterceptor', () => {
  let client: HttpClient;
  let httpMock: HttpTestingController;
  let server: FakeCryptoServer;

  beforeEach(() => {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(withInterceptors([chatCryptoInterceptor])),
        provideHttpClientTesting(),
      ],
    });
    client = TestBed.inject(HttpClient);
    httpMock = TestBed.inject(HttpTestingController);
    server = new FakeCryptoServer();
  });

  /**
   * Đợi request handshake xuất hiện. Số học `@noble` nạp bằng dynamic import
   * (giữ bundle initial dưới ngân sách), nên `keygen()` chạy sau vài vòng vi mô —
   * `expectOne` đồng bộ ngay sau `.subscribe()` sẽ không thấy gì.
   */
  async function pendingHandshake(): Promise<TestRequest> {
    for (let attempt = 0; attempt < 100; attempt++) {
      const [found] = httpMock.match(api('/crypto/handshake'));
      if (found) {
        return found;
      }
      await tick();
    }
    throw new Error('handshake không bao giờ được gửi');
  }

  /** Bắt request handshake (luôn plaintext) và trả dto của server giả */
  async function completeHandshake(): Promise<void> {
    const req = await pendingHandshake();
    expect(req.request.body).toBeTypeOf('object');
    req.flush(ok(server.handshake((req.request.body as { client_public: string }).client_public)));
    await tick();
  }

  it('handshake dựng khoá rồi dán sid vào mọi request trong phạm vi', async () => {
    const seen = vi.fn();
    client.post(api('/chat'), { message: 'hỏi bí mật' }).subscribe(seen);

    const handshake = await pendingHandshake();
    // 43-44 ký tự base64 = đúng 32 byte raw của khoá public X25519
    const clientPublic = (handshake.request.body as { client_public: string }).client_public;
    expect(clientPublic.length).toBeGreaterThanOrEqual(43);
    handshake.flush(ok(server.handshake(clientPublic)));
    await tick();

    const chat = httpMock.expectOne(api('/chat'));
    expect(chat.request.headers.get(SESSION_HEADER)).toBe(server.sid);
    expect(isEnvelope(chat.request.body)).toBe(true);
    chat.flush({ status: true, code: 0, message: 'success', data: { answer: 'ok' } });
    await tick();
    expect(seen).toHaveBeenCalled();
  });

  it('body JSON thành phong bì mở ra đúng nguyên văn, response cũng bị khoá', async () => {
    const payload = { message: 'phụ cấp mấy?', provider: 'groq', limit: 2 };
    const seen = vi.fn();
    client.post(api('/chat'), payload).subscribe(seen);
    await completeHandshake();

    const chat = httpMock.expectOne(api('/chat'));
    const envelope = chat.request.body as CryptoEnvelope;
    expect(envelope.sid).toBe(server.sid);
    expect(server.openRequest(envelope)).toEqual(payload);

    chat.flush(
      server.sealResponse({ status: true, code: 0, message: 'success', data: { answer: '500k' } }),
    );
    await tick();
    expect(seen).toHaveBeenCalledWith({
      status: true,
      code: 0,
      message: 'success',
      data: { answer: '500k' },
    });
  });

  it('GET không body vẫn dán sid và mở được response', async () => {
    const seen = vi.fn();
    client.get(api('/conversations')).subscribe(seen);
    await completeHandshake();

    const list = httpMock.expectOne(api('/conversations'));
    expect(list.request.headers.get(SESSION_HEADER)).toBe(server.sid);
    expect(list.request.body ?? null).toBeNull();
    list.flush(server.sealResponse({ status: true, code: 0, message: 'success', data: [] }));
    await tick();
    expect(seen).toHaveBeenCalledWith({ status: true, code: 0, message: 'success', data: [] });
  });

  it('multipart /chat/attachments giữ plaintext, chỉ response bị khoá', async () => {
    const form = new FormData();
    form.append('file', new Blob(['Nội dung thô'], { type: 'text/plain' }), 'cd.txt');
    const seen = vi.fn();
    client.post(api('/chat/attachments'), form).subscribe(seen);
    await completeHandshake();

    const upload = httpMock.expectOne(api('/chat/attachments'));
    expect(upload.request.body).toBe(form);
    expect(upload.request.headers.get(SESSION_HEADER)).toBe(server.sid);
    upload.flush(
      server.sealResponse({ status: true, code: 0, message: 'success', data: { text: 'x' } }),
    );
    await tick();
    expect(seen).toHaveBeenCalledWith({
      status: true,
      code: 0,
      message: 'success',
      data: { text: 'x' },
    });
  });

  it('sid bị đá → handshake lại và retry ĐÚNG một lần', async () => {
    const seen = vi.fn();
    const failed = vi.fn();
    client.post(api('/chat'), { message: 'một lần nữa' }).subscribe({ next: seen, error: failed });
    await completeHandshake();

    const first = httpMock.expectOne(api('/chat'));
    first.flush(
      { status: false, code: 400, message: 'cần mở lại phiên mã hoá' },
      { status: 400, statusText: 'Bad Request', headers: { [SESSION_HEADER]: 'stale' } },
    );
    await tick();

    // vòng retry phải xin phiên MỚI (server thứ hai) rồi gửi lại đúng một lần
    const second = new FakeCryptoServer();
    const reHandshake = await pendingHandshake();
    reHandshake.flush(
      ok(second.handshake((reHandshake.request.body as { client_public: string }).client_public)),
    );
    await tick();

    const retry = httpMock.expectOne(api('/chat'));
    expect(retry.request.headers.get(SESSION_HEADER)).toBe(second.sid);
    retry.flush(
      second.sealResponse({ status: true, code: 0, message: 'success', data: { answer: 'xong' } }),
    );
    await tick();

    expect(seen).toHaveBeenCalledTimes(1);
    expect(failed).not.toHaveBeenCalled();
    // không còn request nào treo: nếu retry lặp lần ba thì verify() sẽ ném
    httpMock.verify();
  });

  it('lỗi đã niêm phong vẫn giữ status và message cho UI', async () => {
    const failed = vi.fn();
    client.post(api('/chat'), { message: 'sai rồi' }).subscribe({ next: vi.fn(), error: failed });
    await completeHandshake();

    const chat = httpMock.expectOne(api('/chat'));
    chat.flush(server.sealResponse({ status: false, code: 422, message: 'từ khoá lạ' }), {
      status: 422,
      statusText: 'Unprocessable',
    });
    await tick();

    expect(failed).toHaveBeenCalled();
    const error = failed.mock.calls[0][0] as { status: number; error: { message: string } };
    expect(error.status).toBe(422);
    expect(error.error.message).toBe('từ khoá lạ');
  });

  it('handshake hỏng → chạy plaintext, không mã hoá nửa vời', async () => {
    const seen = vi.fn();
    client.post(api('/chat'), { message: 'bn chế độ optional' }).subscribe(seen);

    const handshake = await pendingHandshake();
    handshake.error(new ProgressEvent('error'), { status: 500, statusText: 'Boom' });
    await tick();

    const chat = httpMock.expectOne(api('/chat'));
    expect(chat.request.body).toEqual({ message: 'bn chế độ optional' });
    expect(chat.request.headers.get(SESSION_HEADER)).toBeNull();
    chat.flush({ status: true, code: 0, message: 'success', data: null });
    await tick();
    expect(seen).toHaveBeenCalled();
  });

  it('ngoài phạm vi: không handshake, không header, không đụng body', async () => {
    const seen = vi.fn();
    client.get(api('/users')).subscribe(seen);

    const users = httpMock.expectOne(api('/users'));
    expect(users.request.headers.get(SESSION_HEADER)).toBeNull();
    // đợi vài vòng: nếu inScope sai phạm vi thì handshake (muốn nạp noble trễ) vẫn kịp hiện ra
    await tick();
    await tick();
    expect(httpMock.match(api('/crypto/handshake')).length).toBe(0);
    users.flush({ status: true, code: 0, message: 'success', data: [] });
    await tick();
    expect(seen).toHaveBeenCalled();
  });

  it('mode off từ server → không dựng khoá nữa trong phiên', async () => {
    const seen = vi.fn();
    client.post(api('/chat'), { message: 'server tắt mã hoá' }).subscribe(seen);

    const handshake = await pendingHandshake();
    handshake.flush(ok({ mode: 'off' }));
    await tick();

    const chat = httpMock.expectOne(api('/chat'));
    expect(chat.request.body).toEqual({ message: 'server tắt mã hoá' });
    chat.flush(ok(null));
    await tick();

    const again = vi.fn();
    client.post(api('/chat'), { message: 'lượt hai' }).subscribe(again);
    await tick();
    // mode off là quyết định của server cho cả phiên → không bắt tay lại lần hai
    httpMock.expectNone(api('/crypto/handshake'));
    httpMock.expectOne(api('/chat')).flush(ok(null));
    await tick();
    expect(again).toHaveBeenCalled();
  });
});
