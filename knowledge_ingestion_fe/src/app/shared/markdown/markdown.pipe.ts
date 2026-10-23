import { Pipe, type PipeTransform } from '@angular/core';
import { marked } from 'marked';

/**
 * Markdown → HTML cho câu trả lời của trợ lý. Trả về string thay vì
 * SafeHtml để Angular tự sanitize khi bind `[innerHTML]` (không bypass).
 */
@Pipe({ name: 'markdown' })
export class MarkdownPipe implements PipeTransform {
  transform(value: string): string {
    if (!value) {
      return '';
    }
    return marked.parse(value, { async: false, gfm: true, breaks: true });
  }
}
