import { Component } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { MarkdownPipe } from './markdown.pipe';

const JUMP_LABEL = 'Nhảy tới nguồn trích dẫn';

/**
 * Pipe bind vào `[innerHTML]` — test phải đi qua component thật để ăn luôn bước
 * sanitize của Angular, nếu không sẽ kết luận sai về việc thuộc tính nào sống sót.
 */
@Component({
  selector: 'app-md-host',
  imports: [MarkdownPipe],
  template: '<div class="markdown-body" [innerHTML]="text | markdown: jumpLabel"></div>',
})
class MdHost {
  text = 'Áp dụng theo [^3] nhé.';
  jumpLabel = JUMP_LABEL;
}

describe('MarkdownPipe với trích dẫn [^n]', () => {
  function render(text: string, jumpLabel: string = JUMP_LABEL): HTMLElement {
    const fixture = TestBed.createComponent(MdHost);
    fixture.componentInstance.text = text;
    fixture.componentInstance.jumpLabel = jumpLabel;
    fixture.detectChanges();
    return fixture.nativeElement.querySelector('.markdown-body') as HTMLElement;
  }

  beforeEach(async () => {
    await TestBed.configureTestingModule({ imports: [MdHost] }).compileComponents();
  });

  it('[^3] thành chip superscript bấm được', () => {
    const chip = render('Áp dụng theo [^3] nhé.').querySelector('.md-cite-ref');

    expect(chip?.tagName).toBe('SUP');
    expect(chip?.textContent).toBe('3');
  });

  it('sanitizer giữ class/role/tabindex/aria-label nên chip vẫn là nút bàn phím', () => {
    const chip = render('Theo [^2].').querySelector('.md-cite-ref') as HTMLElement;

    expect(chip.getAttribute('role')).toBe('button');
    expect(chip.getAttribute('tabindex')).toBe('0');
    expect(chip.getAttribute('aria-label')).toBe(JUMP_LABEL);
  });

  it('ngoài khuôn [^n] thì KHÔNG bịa chip', () => {
    const body = render('Kiểu cũ [*2], kiểu số trơn [3], còn [ ^ 1 ] có khoảng trắng.');

    expect(body.querySelector('.md-cite-ref')).toBeNull();
    expect(body.textContent).toContain('[*2]');
    expect(body.textContent).toContain('[3]');
  });

  it('hai marker liền nhau ra hai chip, giữ đúng thứ tự số', () => {
    const chips = render('Tổng hợp [^1][^2] xong.').querySelectorAll('.md-cite-ref');

    expect(chips.length).toBe(2);
    expect(Array.from(chips).map((chip) => chip.textContent)).toEqual(['1', '2']);
  });

  it('markdown thường không bị hồi quy', () => {
    const body = render('**Quan trọng**\n\n- một\n- hai').querySelector('strong');

    expect(body?.textContent).toBe('Quan trọng');
  });

  it('label có ký tự break attribute vẫn nằm nguyên trong aria-label', () => {
    const label = 'Nguồn "trích dẫn" <số>';
    const html = new MarkdownPipe().transform('[^1] ok', label);

    expect(html).toContain(`aria-label="Nguồn &quot;trích dẫn&quot; &lt;số&gt;"`);
  });

  it('chuỗi rỗng trả HTML rỗng', () => {
    expect(new MarkdownPipe().transform('', JUMP_LABEL)).toBe('');
  });
});
