// Setup chung cho unit test (khai báo ở angular.json → architect.test.options.setupFiles).
// jsdom của Angular unit-test builder không expose localStorage ra global —
// bơm từ window nếu có, không thì polyfill in-memory để service dùng được.

type StorageLike = {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
  clear(): void;
  key(index: number): string | null;
  readonly length: number;
};

function memoryStorage(): StorageLike {
  const store = new Map<string, string>();
  return {
    getItem: (key) => store.get(key) ?? null,
    setItem: (key, value) => void store.set(key, String(value)),
    removeItem: (key) => void store.delete(key),
    clear: () => store.clear(),
    key: (index) => [...store.keys()][index] ?? null,
    get length() {
      return store.size;
    },
  };
}

if (typeof (globalThis as { localStorage?: StorageLike }).localStorage === 'undefined') {
  const fromWindow = typeof window !== 'undefined' ? window.localStorage : undefined;
  Object.defineProperty(globalThis, 'localStorage', {
    value: fromWindow ?? memoryStorage(),
    configurable: true,
  });
}
