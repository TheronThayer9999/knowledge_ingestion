/**
 * Helper dựng bộ chuỗi dịch dưới dạng Map.
 *
 * Vì sao không dùng object literal: nguồn dữ liệu dạng mảng tuple không có
 * kiểu "key duy nhất" để TypeScript báo trùng, còn Map thì âm thầm ghi đè.
 * buildMessages chặn cả hai bằng runtime ngay lúc nạp module — trùng key,
 * thiếu key hay key lạ đều fail-fast thay vì lỗi thầm lặng trên UI.
 */

/** 1 entry dịch: [key, giá trị] */
export type MessageEntry<K extends string = string> = readonly [key: K, value: string];

/**
 * Dựng ReadonlyMap từ danh sách entry.
 *
 * @param locale tên bộ (vi/en) — chỉ dùng trong thông báo lỗi
 * @param entries danh sách [key, value] theo đúng thứ tự khai báo
 * @param base bộ canonical để đối chiếu khi dựng bản dịch (đủ key, không key lạ)
 */
export function buildMessages<K extends string>(
  locale: string,
  entries: readonly MessageEntry<K>[],
  base?: ReadonlyMap<K, string>,
): ReadonlyMap<K, string> {
  const messages = new Map<K, string>();
  for (const [key, value] of entries) {
    if (messages.has(key)) {
      throw new Error(`[i18n] Key "${key}" bị trùng trong bộ "${locale}"`);
    }
    messages.set(key, value);
  }

  if (base) {
    for (const key of base.keys()) {
      if (!messages.has(key)) {
        throw new Error(`[i18n] Bộ "${locale}" thiếu key "${key}"`);
      }
    }
    for (const key of messages.keys()) {
      if (!base.has(key)) {
        throw new Error(`[i18n] Bộ "${locale}" có key lạ "${key}"`);
      }
    }
  }

  return messages;
}
