import type { TokenizerAndRendererExtension, Tokens } from 'marked';

/**
 * `marked` core không có footnote, nên `[^n]` (khuôn BE đã chốt ở Đợt 22) bị in
 * nguyên văn ra câu trả lời. Extension này biến đúng khuôn `[^n]` thành chip superscript
 * bấm được — số nguồn lấy từ `n` trong marker, KHÔNG tin model viết gì ngoài khuôn
 * (`[*2]`, `[2]` vẫn là chữ thường, không thành chip).
 *
 * Chip không mang `data-cite`/`id` vì hai thuộc tính đó nằm ngoài allowlist sanitizer
 * của Angular — class/role/tabindex/aria-* thì được giữ, và số nguồn đọc lại từ
 * `textContent` nên FE không cần thuộc tính tùy ý nào.
 */
const CITE_REF = /^\[\^(\d{1,3})\]/;

/** Nhãn i18n đi vào attribute thì phải escape cả 4 ký tự để không phá vỡ thẻ */
function escapeAttr(value: string): string {
  return value
    .replace(/&/g, '&amp;')
    .replace(/"/g, '&quot;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}

export function citationRefExtension(jumpLabel: string): TokenizerAndRendererExtension {
  return {
    name: 'citeRef',
    level: 'inline',
    // Báo cho marked vị trí candidate gần nhất — thiếu `start` thì marker bị ăn vào
    // text thường và không bao giờ gọi tokenizer.
    start: (src: string) => src.indexOf('[^'),
    tokenizer: (src: string) => {
      const match = CITE_REF.exec(src);
      if (!match) {
        return undefined;
      }
      return { type: 'citeRef', raw: match[0], citeN: match[1] };
    },
    renderer: (token: Tokens.Generic) =>
      // `Tokens.Generic` là index signature nên TS bắt truy cập bằng ngoặc vuông
      `<sup class="md-cite-ref" role="button" tabindex="0" aria-label="${escapeAttr(
        jumpLabel,
      )}">${token['citeN']}</sup>`,
  };
}
