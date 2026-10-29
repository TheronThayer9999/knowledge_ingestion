/**
 * Trợ giúp dùng chung cho 2 form provider (popup admin + popup "Kết nối của tôi").
 *
 * Sau khi user chỉ cần chọn provider trong danh mục + dán API key, hai form KHÔNG
 * còn cùng bộ ô nên không gộp được thành một component — nhưng luật "chỉ gửi
 * field có giá trị" và cách đọc giá trị ô nhập thì vẫn giống nhau, nên tách ra
 * đây cho một chỗ.
 */

/** Đọc giá trị ô input/textarea từ event (selectionChange cũng trả về string). */
export function valueOf(event: Event): string {
  return (event.target as HTMLInputElement).value;
}

/**
 * Bỏ field rỗng khỏi form trước khi gửi — backend hiểu "không gửi = giữ giá
 * trị cũ", nên gửi chuỗi rỗng sẽ vô tình xoá nội dung đã lưu.
 *
 * CHỈ bỏ `undefined` và `''` — `false` vẫn được giữ (cờ "tắt provider" = false
 * là thay đổi thật, không phải "không gửi").
 */
export function compactForm<T extends object>(form: T): T {
  const out: Record<string, unknown> = { ...(form as Record<string, unknown>) };
  for (const key of Object.keys(out)) {
    const value = out[key];
    if (value === undefined || value === '') {
      delete out[key];
    }
  }
  return out as T;
}
