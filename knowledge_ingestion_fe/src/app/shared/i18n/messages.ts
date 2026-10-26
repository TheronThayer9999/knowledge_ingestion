/**
 * Helper dựng bộ chuỗi dịch dưới dạng Map từ một resource object.
 *
 * Trước đây dùng mảng tuple + buildMessages đối chiếu runtime (thiếu key, trùng
 * key, key lạ) lúc nạp module. Nay các file i18n là object literal và en.ts
 * khai báo `Record<TranslationKey, string>` — TypeScript đã chặn thiếu key, key
 * lạ và trùng key ngay lúc biên dịch, nên ở đây chỉ còn bước convert object →
 * Map cho LangService tra cứu, không cần kiểm tra nữa.
 */
export function toMessages<K extends string>(
  source: Readonly<Record<K, string>>,
): ReadonlyMap<K, string> {
  return new Map(Object.entries(source) as [K, string][]);
}
