/**
 * Hoạt cảnh "đang gõ" cho đáp án (Đợt 27).
 *
 * Backend trả cả khối markdown một lần (không SSE), nên FE tự hé lộ dần. Hai
 * bất biến chi phối cách chia mảnh:
 * - GHÉP LẠI ĐÚNG BẰNG CHUỖI GỐC: đáp án hiển thị xong phải từng ký tự như khi
 *   chưa hoạt cảnh, nếu không MarkdownPipe sẽ sinh chip trích dẫn khác đi.
 * - KHÔNG BAO GIỜ CẮT GIỮA MARKER `[^n]`: nửa marker (`[^`) bị marked hiểu là
 *   chữ thường, chip nguồn sẽ nháy lên rồi đổi form giữa lúc gõ.
 */

/** Số nhịp mong muốn cho một đáp án — ~45 nhịp * 33ms ≈ 1,5s, dài hơn vẫn kịp đọc */
const TARGET_TICKS = 45;

/**
 * Mảnh = marker `[^n]` nguyên khối, hoặc một từ, hoặc một cụm khoảng trắng.
 * Khoảng trắng đi kèm từ ĐỨNG SAU khi từ trước nó là marker — nhờ vậy nhịp
 * marker hiện đúng `kho [^1]` rồi nhịp sau mới thêm ` thành`, chứ không có nhịp
 * chỉ có dấu cách (màn hình không đổi gì mà vẫn tốn một nhịp).
 */
const TOKEN_PATTERN = /\[\^\d+\]|\S+|\s+/g;
const MARKER_PATTERN = /^\[\^\d+\]$/;

export function splitRevealChunks(text: string): string[] {
  const chunks: string[] = [];
  let pendingSpace = '';
  for (const token of text.match(TOKEN_PATTERN) ?? []) {
    if (/^\s+$/.test(token)) {
      const previous = chunks[chunks.length - 1];
      if (pendingSpace === '' && previous !== undefined && !MARKER_PATTERN.test(previous)) {
        chunks[chunks.length - 1] += token;
      } else {
        pendingSpace += token;
      }
      continue;
    }
    chunks.push(pendingSpace + token);
    pendingSpace = '';
  }
  if (pendingSpace !== '') {
    chunks.push(pendingSpace);
  }
  return chunks;
}

/**
 * Đáp án 5 từ thì gõ 1 từ/nhịp cho chậm rãi; 500 từ phải tăng tốc để không mất
 * 30 giây — perTick tỷ lệ với độ dài, tối thiểu 1.
 */
export function perTickFor(chunkCount: number): number {
  return Math.max(1, Math.ceil(chunkCount / TARGET_TICKS));
}

/** Máy trạng thái thuần: không timer, không DOM — component giữ nhịp */
export class StreamReveal {
  private readonly text: string;
  /** Độ dài prefix sau mỗi mảnh, tính một lần để `step()` chỉ slice */
  private readonly ends: number[];
  private index = 0;

  constructor(
    chunks: string[],
    private readonly perTick: number,
  ) {
    this.text = chunks.join('');
    let acc = 0;
    this.ends = chunks.map((chunk) => (acc += chunk.length));
  }

  get isComplete(): boolean {
    return this.index >= this.ends.length;
  }

  /** Tiến thêm `perTick` mảnh và trả nội dung hiển thị tại nhịp đó */
  step(): string {
    this.index = Math.min(this.index + Math.max(1, this.perTick), this.ends.length);
    return this.preview();
  }

  preview(): string {
    return this.text.slice(0, this.ends[this.index - 1] ?? 0);
  }

  /** Chuỗi cuối cùng — bằng đúng những gì hoạt cảnh che đi */
  full(): string {
    return this.text;
  }
}

/**
 * Dựng hoạt cảnh từ markdown gốc. Trả `null` khi không có gì để gõ (rỗng) để
 * phía gọi phân biệt "không chạy" với "chạy xong ngay".
 */
export function createStreamReveal(text: string): StreamReveal | null {
  const chunks = splitRevealChunks(text);
  if (chunks.length === 0) {
    return null;
  }
  return new StreamReveal(chunks, perTickFor(chunks.length));
}
