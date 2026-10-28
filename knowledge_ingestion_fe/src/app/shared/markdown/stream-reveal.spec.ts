import { createStreamReveal, perTickFor, splitRevealChunks, StreamReveal } from './stream-reveal';

describe('splitRevealChunks — chia mảnh không cắt đôi marker trích dẫn', () => {
  it('ghép lại từng ký tự bằng chuỗi gốc (đáp án sau hoạt cảnh không đổi)', () => {
    const text = 'Quy chế áp dụng từ 01/01[^2].\n\n- Điều 3 nói về phụ cấp[^1]\n';
    expect(splitRevealChunks(text).join('')).toBe(text);
  });

  it('không mảnh nào chứa nửa marker `[^` hoặc `]` lạc lõng', () => {
    const text = 'a [^12] b [^3] c[^9]d';
    for (const chunk of splitRevealChunks(text)) {
      expect(chunk).not.toMatch(/\[\^\d*$/);
      expect(chunk).not.toMatch(/^\s*\d*\]/);
    }
  });

  it('marker đứng một mình là nguyên một mảnh', () => {
    expect(splitRevealChunks('kho [^1] thành')).toEqual(['kho ', '[^1]', ' thành']);
  });

  it('chuỗi rỗng không có mảnh nào', () => {
    expect(splitRevealChunks('')).toEqual([]);
  });
});

describe('perTickFor — tốc độ theo độ dài', () => {
  it('đoản câu gõ một từ mỗi nhịp', () => {
    expect(perTickFor(5)).toBe(1);
  });

  it('đáp án dài vẫn kết thúc trong số nhịp mục tiêu', () => {
    expect(perTickFor(450)).toBe(10);
  });
});

describe('StreamReveal — máy bước thuần', () => {
  const make = (text: string, perTick: number): StreamReveal =>
    new StreamReveal(splitRevealChunks(text), perTick);

  it('step trả prefix tăng dần và full khớp chuỗi gốc', () => {
    const reveal = make('một hai ba bốn', 2);
    expect(reveal.preview()).toBe('');
    expect(reveal.step()).toBe('một hai ');
    expect(reveal.step()).toBe('một hai ba bốn');
    expect(reveal.isComplete).toBe(true);
    expect(reveal.full()).toBe('một hai ba bốn');
  });

  it('complete rồi step thêm cũng không tràn ra ngoài chuỗi', () => {
    const reveal = make('x y', 1);
    reveal.step();
    reveal.step();
    expect(reveal.isComplete).toBe(true);
    expect(reveal.step()).toBe('x y');
  });

  it('perTick <= 0 được coi là 1 để không đứng tại chỗ', () => {
    const reveal = new StreamReveal(splitRevealChunks('a b c'), 0);
    expect(reveal.step()).toBe('a ');
  });

  it('createStreamReveal trả null khi không có nội dung để gõ', () => {
    expect(createStreamReveal('')).toBeNull();
    expect(createStreamReveal('   ')).not.toBeNull();
  });
});
