import { describe, expect, it } from 'vitest';
import { compactForm, valueOf } from './llm.form';

describe('compactForm — chỉ gửi field có giá trị', () => {
  it('bỏ chuỗi rỗng và undefined, giữ lại phần còn lại', () => {
    const form = compactForm({ label: 'Groq', base_url: '', api_key: undefined, model: 'm1' });
    expect(form).toEqual({ label: 'Groq', model: 'm1' });
  });

  it('GIỮ false — cờ tắt provider là thay đổi thật, không phải "không gửi"', () => {
    const form = compactForm({ disabled: false });
    expect(form).toEqual({ disabled: false });
    expect('disabled' in form).toBe(true);
  });

  it('không sửa đổi object gốc', () => {
    const original = { label: '', model: 'm1' };
    compactForm(original);
    expect(original).toEqual({ label: '', model: 'm1' });
  });
});

describe('valueOf — đọc giá trị ô nhập', () => {
  it('lấy value từ input', () => {
    const input = document.createElement('input');
    input.value = 'abc';
    expect(valueOf({ target: input } as unknown as Event)).toBe('abc');
  });
});
