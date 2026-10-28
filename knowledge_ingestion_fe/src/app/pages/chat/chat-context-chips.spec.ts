import type { ComponentFixture } from '@angular/core/testing';
import { TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MatMenuTrigger } from '@angular/material/menu';
import { MatTooltip } from '@angular/material/tooltip';
import { describe, expect, it, vi } from 'vitest';
import type { Category } from '../../core/knowledge/knowledge.models';
import { LangService } from '../../core/i18n/lang.service';
import type { LlmProviderOption } from '../../core/llm/llm.models';
import { ChatContextChips } from './chat-context-chips';

const CATEGORIES: Category[] = [
  { id: 4, name: 'Hợp đồng', description: '', parentId: null, depth: 0 },
];

const PROVIDERS: LlmProviderOption[] = [
  {
    slug: '9router',
    label: '9Router',
    type: 'openai',
    defaultModel: 'nemotron',
    models: ['nemotron', 'gpt-oss'],
    available: true,
    thinking: true,
    isDefault: true,
  },
  {
    slug: 'groq',
    label: 'Groq',
    type: 'openai',
    defaultModel: 'llama-3.3-70b',
    models: [],
    available: true,
    thinking: false,
    isDefault: false,
  },
];

interface Setup {
  fixture: ComponentFixture<ChatContextChips>;
  lang: LangService;
}

function setup(inputs: {
  scopeId?: number;
  scopeLabel?: string;
  thinking?: boolean;
  locked?: boolean;
  busy?: boolean;
  providers?: LlmProviderOption[];
  provider?: string;
  model?: string;
  thinkingSupported?: boolean;
}): Setup {
  TestBed.resetTestingModule();
  TestBed.configureTestingModule({ imports: [ChatContextChips] });
  const fixture = TestBed.createComponent(ChatContextChips);
  const lang = TestBed.inject(LangService);
  lang.setLang('vi');
  fixture.componentRef.setInput('scopeId', inputs.scopeId ?? 0);
  fixture.componentRef.setInput('scopeLabel', inputs.scopeLabel ?? 'Toàn bộ kho');
  fixture.componentRef.setInput('thinking', inputs.thinking ?? false);
  fixture.componentRef.setInput('locked', inputs.locked ?? false);
  fixture.componentRef.setInput('busy', inputs.busy ?? false);
  fixture.componentRef.setInput('categories', CATEGORIES);
  fixture.componentRef.setInput('providers', inputs.providers ?? []);
  fixture.componentRef.setInput('provider', inputs.provider ?? '');
  fixture.componentRef.setInput('model', inputs.model ?? '');
  fixture.componentRef.setInput('thinkingSupported', inputs.thinkingSupported ?? true);
  fixture.detectChanges();
  return { fixture, lang };
}

function chip(fixture: ComponentFixture<ChatContextChips>, index: number): HTMLButtonElement {
  const element = fixture.nativeElement as HTMLElement;
  return element.querySelectorAll<HTMLButtonElement>('.chip')[index];
}

function tooltipOf(fixture: ComponentFixture<ChatContextChips>, index: number): string {
  const button = fixture.debugElement.queryAll(By.directive(MatTooltip))[index];
  return button.injector.get(MatTooltip).message;
}

describe('ChatContextChips — nhãn trạng thái', () => {
  it('hiện phạm vi hiện tại và trạng thái suy luận sâu', () => {
    const { fixture, lang } = setup({ scopeLabel: 'Hợp đồng', thinking: true });
    const element = fixture.nativeElement as HTMLElement;

    expect(element.querySelector('.chip-value')?.textContent?.trim()).toBe('Hợp đồng');
    expect(element.querySelectorAll('.chip-value')[1].textContent?.trim()).toBe(lang.t('CHAT_ON'));
    expect(chip(fixture, 1).getAttribute('aria-pressed')).toBe('true');
    expect(chip(fixture, 1).classList.contains('is-active')).toBe(true);
  });

  it('nút phiên mới phát newSession', () => {
    const { fixture } = setup({});
    const component = fixture.componentInstance;
    const emit = vi.fn();
    component.newSession.subscribe(emit);

    (fixture.nativeElement as HTMLElement)
      .querySelector<HTMLButtonElement>('.chip-action')!
      .click();

    expect(emit).toHaveBeenCalled();
  });
});

describe('ChatContextChips — chọn phạm vi khi phiên chưa khoá', () => {
  it('mở menu, tick mục đang chọn và phát id khi chọn danh mục', async () => {
    const { fixture } = setup({ scopeId: 0 });
    const component = fixture.componentInstance;
    const picked: number[] = [];
    component.pickScope.subscribe((id) => picked.push(id));

    fixture.debugElement
      .query(By.directive(MatMenuTrigger))
      .injector.get(MatMenuTrigger)
      .openMenu();
    fixture.detectChanges();
    await new Promise((resolve) => setTimeout(resolve));

    const items = Array.from(document.querySelectorAll<HTMLButtonElement>('.mat-mdc-menu-item'));
    expect(items).toHaveLength(CATEGORIES.length + 1);
    // Mục "Toàn bộ kho" đang chọn có dấu tick, danh mục thì không
    expect(items[0].querySelector('mat-icon')).not.toBeNull();
    expect(items[1].querySelector('mat-icon')).toBeNull();

    items[1].click();
    expect(picked).toEqual([4]);
  });
});

describe('ChatContextChips — khoá mềm (nhóm 5 + 7)', () => {
  it('phiên đã có tin nhắn: chip khoá, tooltip nói tạo phiên mới, click KHÔNG phát output', () => {
    const { fixture, lang } = setup({ locked: true });

    const scope = chip(fixture, 0);
    const thinking = chip(fixture, 1);
    expect(scope.getAttribute('aria-disabled')).toBe('true');
    expect(thinking.getAttribute('aria-disabled')).toBe('true');
    expect(scope.classList.contains('is-locked')).toBe(true);
    // Khoá mềm vẫn focus được để đọc tooltip
    expect(scope.disabled).toBe(false);

    expect(tooltipOf(fixture, 0)).toBe(lang.t('CHAT_CHIP_LOCKED_HINT'));
    expect(tooltipOf(fixture, 1)).toBe(lang.t('CHAT_CHIP_LOCKED_HINT'));

    const pick = vi.fn();
    const toggle = vi.fn();
    fixture.componentInstance.pickScope.subscribe(pick);
    fixture.componentInstance.toggleThinking.subscribe(toggle);

    scope.click();
    thinking.click();

    expect(pick).not.toHaveBeenCalled();
    expect(toggle).not.toHaveBeenCalled();
  });

  it('đang chờ đáp án cũng chặn đổi dù phiên chưa có lượt nào', () => {
    const { fixture } = setup({ busy: true });
    const toggle = vi.fn();
    fixture.componentInstance.toggleThinking.subscribe(toggle);

    expect(chip(fixture, 1).getAttribute('aria-disabled')).toBe('true');
    chip(fixture, 1).click();

    expect(toggle).not.toHaveBeenCalled();
  });

  it('phiên chưa khoá: bấm chip suy luận sâu phát toggle', () => {
    const { fixture } = setup({ locked: false });
    const toggle = vi.fn();
    fixture.componentInstance.toggleThinking.subscribe(toggle);

    chip(fixture, 1).click();

    expect(toggle).toHaveBeenCalled();
  });
});

describe('ChatContextChips — khoá "Suy luận sâu" theo năng lực model (Đợt 28)', () => {
  it('model không hỗ trợ: chip khoá, tooltip nói đúng lý do, bấm KHÔNG phát toggle', () => {
    const { fixture, lang } = setup({ thinkingSupported: false });
    const toggle = vi.fn();
    fixture.componentInstance.toggleThinking.subscribe(toggle);

    const button = chip(fixture, 1);
    expect(button.getAttribute('aria-disabled')).toBe('true');
    expect(button.classList.contains('is-locked')).toBe(true);
    // Lý do phải là "model không hỗ trợ", không phải "tạo phiên mới"
    expect(tooltipOf(fixture, 1)).toBe(lang.t('CHAT_THINKING_UNSUPPORTED'));

    button.click();
    expect(toggle).not.toHaveBeenCalled();
  });

  it('đang BẬT mà model không hỗ trợ (phiên cũ) thì vẫn tắt được', () => {
    const { fixture } = setup({ thinking: true, thinkingSupported: false });
    const toggle = vi.fn();
    fixture.componentInstance.toggleThinking.subscribe(toggle);

    expect(chip(fixture, 1).getAttribute('aria-disabled')).toBe('false');
    chip(fixture, 1).click();

    expect(toggle).toHaveBeenCalled();
  });

  it('model có hỗ trợ: chip sáng và bấm được như thường', () => {
    const { fixture, lang } = setup({ thinkingSupported: true });
    const toggle = vi.fn();
    fixture.componentInstance.toggleThinking.subscribe(toggle);

    expect(chip(fixture, 1).getAttribute('aria-disabled')).toBe('false');
    expect(tooltipOf(fixture, 1)).toBe(lang.t('CHAT_THINKING_HINT'));
    chip(fixture, 1).click();

    expect(toggle).toHaveBeenCalled();
  });
});

describe('ChatContextChips — chip mô hình (Đợt 28)', () => {
  it('không có provider (options lỗi) thì ẩn hẳn chip mô hình', () => {
    const { fixture } = setup({ providers: [] });
    const element = fixture.nativeElement as HTMLElement;

    // Chỉ còn chip phạm vi + chip suy luận sâu + nút phiên mới
    expect(element.querySelectorAll('.chip').length).toBe(2);
    expect(element.textContent).not.toContain('Mô hình');
  });

  it('có provider thì hiện chip, nhãn mặc định là "Theo cấu hình"', () => {
    const { fixture, lang } = setup({ providers: PROVIDERS });
    const element = fixture.nativeElement as HTMLElement;

    expect(element.querySelectorAll('.chip').length).toBe(3);
    expect(element.querySelectorAll('.chip-value')[2].textContent?.trim()).toBe(
      lang.t('CHAT_MODEL_CONFIG'),
    );
  });

  it('đã chọn provider+model thì nhãn chip là "Provider · model"', () => {
    const { fixture } = setup({ providers: PROVIDERS, provider: '9router', model: 'gpt-oss' });
    const element = fixture.nativeElement as HTMLElement;

    expect(element.querySelectorAll('.chip-value')[2].textContent?.trim()).toBe(
      '9Router · gpt-oss',
    );
    expect(element.querySelectorAll('.chip')[2].classList.contains('is-active')).toBe(true);
  });

  it('menu nhóm theo provider: nhãn nhóm là DIV tĩnh, model mới là mục bấm', async () => {
    const { fixture, lang } = setup({ providers: PROVIDERS });
    const component = fixture.componentInstance;
    const picked: { provider: string; model: string }[] = [];
    component.pickModel.subscribe((choice) => picked.push(choice));

    fixture.debugElement
      .queryAll(By.directive(MatMenuTrigger))[1]
      .injector.get(MatMenuTrigger)
      .openMenu();
    fixture.detectChanges();
    await new Promise((resolve) => setTimeout(resolve));

    const items = Array.from(document.querySelectorAll<HTMLButtonElement>('.mat-mdc-menu-item'));
    // Chỉ còn các mục CHỌN được: "Theo cấu hình" + 3 model (9router 2, groq 1).
    // Tên provider trước đây là <button disabled> nên bị đếm nhầm thành mục bị
    // hỏng — giờ nó là div nhãn nhóm.
    expect(items.length).toBe(4);
    expect(items[0].textContent).toContain(lang.t('CHAT_MODEL_CONFIG'));
    expect(items[1].textContent).toContain('nemotron');
    expect(items[2].textContent).toContain('gpt-oss');

    const headings = Array.from(document.querySelectorAll('.menu-heading'));
    expect(headings.map((el) => el.textContent?.trim())).toEqual(['9Router', 'Groq']);
    expect(headings[0].tagName).toBe('DIV');

    items[2].click();
    expect(picked).toEqual([{ provider: '9router', model: 'gpt-oss' }]);
  });

  it('provider chỉ có 1 model (không khai models) thì menu vẫn hiện model đó', async () => {
    const { fixture } = setup({ providers: PROVIDERS });
    fixture.debugElement
      .queryAll(By.directive(MatMenuTrigger))[1]
      .injector.get(MatMenuTrigger)
      .openMenu();
    fixture.detectChanges();
    await new Promise((resolve) => setTimeout(resolve));

    const items = Array.from(document.querySelectorAll<HTMLButtonElement>('.mat-mdc-menu-item'));
    // groq không khai models → menu vẫn liệt kê đúng model mặc định của nó
    expect(items[3].textContent).toContain('llama-3.3-70b');
  });

  it('bấm "Theo cấu hình" phát provider/model rỗng (không ghi đè lựa chọn)', async () => {
    const { fixture } = setup({ providers: PROVIDERS, provider: '9router', model: 'gpt-oss' });
    const component = fixture.componentInstance;
    const picked: { provider: string; model: string }[] = [];
    component.pickModel.subscribe((choice) => picked.push(choice));

    fixture.debugElement
      .queryAll(By.directive(MatMenuTrigger))[1]
      .injector.get(MatMenuTrigger)
      .openMenu();
    fixture.detectChanges();
    await new Promise((resolve) => setTimeout(resolve));

    document.querySelectorAll<HTMLButtonElement>('.mat-mdc-menu-item')[0].click();
    expect(picked).toEqual([{ provider: '', model: '' }]);
  });

  it('chip mô hình KHÔNG bị khoá theo phiên (backend nhận provider mỗi lượt)', () => {
    const { fixture } = setup({ providers: PROVIDERS, locked: true });
    const model = chip(fixture, 2);

    expect(model.getAttribute('aria-disabled')).toBeNull();
    expect(model.classList.contains('is-locked')).toBe(false);
  });
});
