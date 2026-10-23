/** Envelope chuẩn của backend: {status, code, message, data} — code 0 là thành công */
export interface ApiEnvelope<T> {
  status: boolean;
  code: number;
  message: string;
  data: T;
}

/**
 * Lỗi API đã chuẩn hoá cho tầng UI:
 * - status = HTTP status (0 khi không kết nối được server)
 * - code = mã nghiệp vụ trong body (thường trùng status)
 * - message = message server trả về (đã là tiếng Việt, được phép hiển thị)
 */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: number,
    message: string,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}
