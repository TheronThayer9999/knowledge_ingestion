import { Pipe, type PipeTransform } from '@angular/core';
import { Marked } from 'marked';
import { citationRefExtension } from './citation-ref.extension';

/**
 * Mỗi nhãn trích dẫn (vi/en) một parser — `marked.use` sửa singleton của cả app nên
 * ở đây tạo `Marked` riêng, nhớ kết quả vào Map để không dựng lại parser mỗi lượt
 * change detection (pipe pure được gọi liên tục).
 */
const parsers = new Map<string, Marked>();

function parserFor(jumpLabel: string): Marked {
  const cached = parsers.get(jumpLabel);
  if (cached) {
    return cached;
  }
  const marked = new Marked({ gfm: true, breaks: true });
  marked.use({ extensions: [citationRefExtension(jumpLabel)] });
  parsers.set(jumpLabel, marked);
  return marked;
}

/**
 * Markdown → HTML cho câu trả lời của trợ lý. Trả về string thay vì
 * SafeHtml để Angular tự sanitize khi bind `[innerHTML]` (không bypass).
 * `jumpLabel` là nhãn i18n gắn vào chip trích dẫn `[^n]`.
 */
@Pipe({ name: 'markdown' })
export class MarkdownPipe implements PipeTransform {
  transform(value: string, jumpLabel: string): string {
    if (!value) {
      return '';
    }
    return parserFor(jumpLabel).parse(value, { async: false });
  }
}
