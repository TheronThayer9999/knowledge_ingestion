/**
 * Accessor an toàn cho các global phía browser (localStorage/document/navigator).
 * Unit test (vitest) chạy trong môi trường không có DOM đầy đủ và SSR không có
 * browser global — service khởi tạo ở đó không được phép crash.
 */
export function safeLocalStorage(): Storage | null {
  return typeof globalThis.localStorage === 'undefined' ? null : globalThis.localStorage;
}

export function safeDocument(): Document | null {
  return typeof globalThis.document === 'undefined' ? null : globalThis.document;
}

export function browserLangPrefix(): string | null {
  if (typeof globalThis.navigator === 'undefined') {
    return null;
  }
  return globalThis.navigator.language?.slice(0, 2) ?? null;
}
