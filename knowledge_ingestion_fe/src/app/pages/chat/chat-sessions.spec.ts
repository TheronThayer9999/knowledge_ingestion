import type { ComponentFixture } from '@angular/core/testing';
import { TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { MatMenuTrigger } from '@angular/material/menu';
import { describe, expect, it, vi } from 'vitest';
import type { Conversation } from '../../core/chat/chat.models';
import { LangService, type TranslationKey } from '../../core/i18n/lang.service';
import { ChatSessions } from './chat-sessions';

const NOW = Date.now();

function session(id: number, ageMs: number, overrides: Partial<Conversation> = {}): Conversation {
  return {
    id,
    title: `Phiên ${id}`,
    provider: '',
    model: '',
    thinking: false,
    categoryIds: [],
    messageCount: 2,
    lastMessageAt: new Date(NOW - ageMs),
    createdAt: new Date(NOW - ageMs),
    ...overrides,
  };
}

const HOUR = 3_600_000;
const DAY = 24 * HOUR;

interface Setup {
  fixture: ComponentFixture<ChatSessions>;
  lang: LangService;
}

function setup(inputs: {
  conversations: Conversation[];
  activeId?: number | null;
  answering?: boolean;
  loading?: boolean;
  failed?: boolean;
  hasMore?: boolean;
  suggestions?: TranslationKey[];
}): Setup {
  TestBed.resetTestingModule();
  TestBed.configureTestingModule({ imports: [ChatSessions] });
  const fixture = TestBed.createComponent(ChatSessions);
  const lang = TestBed.inject(LangService);
  lang.setLang('vi');
  fixture.componentRef.setInput('conversations', inputs.conversations);
  fixture.componentRef.setInput('activeId', inputs.activeId ?? null);
  fixture.componentRef.setInput('answering', inputs.answering ?? false);
  fixture.componentRef.setInput('loading', inputs.loading ?? false);
  fixture.componentRef.setInput('failed', inputs.failed ?? false);
  fixture.componentRef.setInput('hasMore', inputs.hasMore ?? false);
  fixture.componentRef.setInput('suggestions', inputs.suggestions ?? []);
  fixture.detectChanges();
  return { fixture, lang };
}

function text(element: Element | null): string {
  return element?.textContent?.trim() ?? '';
}

describe('ChatSessions — danh sách nhóm theo ngày', () => {
  it('chia nhóm Hôm nay / Hôm qua / Cũ hơn và bỏ nhóm trống', () => {
    const { fixture, lang } = setup({
      conversations: [session(1, 40 * DAY), session(2, 2 * HOUR), session(3, DAY + HOUR)],
    });
    const element = fixture.nativeElement as HTMLElement;
    const headings = Array.from(element.querySelectorAll('.group-heading')).map(text);

    expect(headings).toEqual([
      lang.t('CHAT_GROUP_TODAY'),
      lang.t('CHAT_GROUP_YESTERDAY'),
      lang.t('CHAT_GROUP_OLDER'),
    ]);
    // Phiên hoạt động gần nhất đứng đầu nhóm đầu
    expect(element.querySelector('.session .session-title')?.textContent?.trim()).toBe('Phiên 2');
  });

  it('dòng meta in thời gian tương đối + số tin nhắn, không còn "lượt"', () => {
    const { fixture, lang } = setup({
      conversations: [session(1, 5 * 60_000, { messageCount: 7 })],
    });
    const meta = text((fixture.nativeElement as HTMLElement).querySelector('.session-meta'));

    expect(meta).toContain(lang.t('CHAT_TIME_MIN').replace('{n}', '5'));
    expect(meta).toContain(lang.t('CHAT_SESSION_MESSAGES').replace('{n}', '7'));
  });

  it('phiên đang mở có aria-current và class is-active', () => {
    const { fixture } = setup({ conversations: [session(1, HOUR)], activeId: 1 });
    const row = (fixture.nativeElement as HTMLElement).querySelector('.session');

    expect(row?.classList.contains('is-active')).toBe(true);
    expect(row?.getAttribute('aria-current')).toBe('true');
  });
});

describe('ChatSessions — đổi tên inline (nhóm 1 + 4)', () => {
  it('Enter lưu: phát title đã trim, đóng ô nhập', () => {
    const { fixture } = setup({ conversations: [session(1, HOUR)] });
    const component = fixture.componentInstance;
    const renamed: { id: number; title: string }[] = [];
    component.rename.subscribe((value) => renamed.push(value));

    component['beginRename'](component.conversations()[0]);
    fixture.detectChanges();
    const input = (fixture.nativeElement as HTMLElement).querySelector<HTMLInputElement>(
      '.session-rename',
    );
    expect(input).not.toBeNull();
    // Ô nhập nhận focus ngay (afterNextRender) để gõ được không cần Tab thêm
    expect(document.activeElement).toBe(input);

    input!.value = '  Tên mới  ';
    input!.dispatchEvent(new Event('input'));
    input!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));
    fixture.detectChanges();

    expect(renamed).toEqual([{ id: 1, title: 'Tên mới' }]);
    expect((fixture.nativeElement as HTMLElement).querySelector('.session-rename')).toBeNull();
  });

  it('Esc huỷ: không phát rename và trả lại hàng', () => {
    const { fixture } = setup({ conversations: [session(1, HOUR)] });
    const component = fixture.componentInstance;
    const rename = vi.fn();
    component.rename.subscribe(rename);

    component['beginRename'](component.conversations()[0]);
    fixture.detectChanges();
    const input = (fixture.nativeElement as HTMLElement).querySelector<HTMLInputElement>(
      '.session-rename',
    );
    input!.value = 'Không lưu';
    input!.dispatchEvent(new Event('input'));
    input!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    fixture.detectChanges();

    expect(rename).not.toHaveBeenCalled();
    expect((fixture.nativeElement as HTMLElement).querySelector('.session-rename')).toBeNull();
  });

  it('title không đổi thì không phát output (tránh PATCH vô nghĩa)', () => {
    const { fixture } = setup({ conversations: [session(1, HOUR, { title: 'Giữ nguyên' })] });
    const component = fixture.componentInstance;
    const rename = vi.fn();
    component.rename.subscribe(rename);

    component['beginRename'](component.conversations()[0]);
    fixture.detectChanges();
    (fixture.nativeElement as HTMLElement)
      .querySelector('.session-rename')!
      .dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));

    expect(rename).not.toHaveBeenCalled();
  });
});

describe('ChatSessions — menu ⋯ và trạng thái đang trả lời (nhóm 4 + 8)', () => {
  it('menu có mục "Xoá phiên" mang class đỏ và vẫn nằm trong cây khi chưa hover', async () => {
    const { fixture } = setup({ conversations: [session(1, HOUR)] });
    const element = fixture.nativeElement as HTMLElement;
    const trigger = fixture.debugElement
      .query(By.directive(MatMenuTrigger))
      .injector.get(MatMenuTrigger);

    // Ẩn bằng opacity nên nút phải còn trong DOM/thứ tự Tab (không display: none)
    const menuButton = element.querySelector('.session-menu');
    expect(menuButton).not.toBeNull();
    expect(menuButton?.closest('[hidden]')).toBeNull();

    trigger.openMenu();
    fixture.detectChanges();
    await new Promise((resolve) => setTimeout(resolve));

    const danger = document.querySelector('.menu-danger');
    expect(danger).not.toBeNull();
    expect(text(danger)).toContain(TestBed.inject(LangService).t('CHAT_DELETE'));
  });

  it('đang chờ đáp án: khoá nút mở + menu, gắn chấm trạng thái có nhãn', () => {
    const { fixture, lang } = setup({
      conversations: [session(1, HOUR)],
      activeId: 1,
      answering: true,
    });
    const element = fixture.nativeElement as HTMLElement;

    expect(element.querySelector<HTMLButtonElement>('.session-main')?.disabled).toBe(true);
    expect(element.querySelector<HTMLButtonElement>('.session-menu')?.disabled).toBe(true);
    const dot = element.querySelector('.status-dot');
    expect(dot?.getAttribute('role')).toBe('status');
    expect(dot?.getAttribute('aria-label')).toBe(lang.t('CHAT_SESSION_PROCESSING'));
  });

  it('chấm trạng thái chỉ gắn lên phiên đang mở', () => {
    const { fixture } = setup({
      conversations: [session(1, HOUR), session(2, 2 * HOUR)],
      activeId: 1,
      answering: true,
    });

    expect((fixture.nativeElement as HTMLElement).querySelectorAll('.status-dot')).toHaveLength(1);
  });
});

describe('ChatSessions — empty state, lỗi và tải thêm', () => {
  it('rỗng: gợi ý mở phiên + câu hỏi mẫu phát askExample', () => {
    const { fixture } = setup({
      conversations: [],
      suggestions: ['CHAT_SUGGEST_1', 'CHAT_SUGGEST_2'],
    });
    const component = fixture.componentInstance;
    const asked: TranslationKey[] = [];
    component.askExample.subscribe((key) => asked.push(key));

    const examples = (fixture.nativeElement as HTMLElement).querySelectorAll<HTMLButtonElement>(
      '.session-example',
    );
    expect(examples).toHaveLength(2);
    examples[1].click();

    expect(asked).toEqual(['CHAT_SUGGEST_2']);
  });

  it('lỗi: hiện thông báo và nút thử lại phát retry, không hiện empty state', () => {
    const { fixture } = setup({ conversations: [], failed: true });
    const component = fixture.componentInstance;
    const retry = vi.fn();
    component.retry.subscribe(retry);

    expect((fixture.nativeElement as HTMLElement).querySelector('.sessions-empty')).toBeNull();
    (fixture.nativeElement as HTMLElement)
      .querySelector<HTMLButtonElement>('.panel-action')!
      .click();

    expect(retry).toHaveBeenCalled();
  });

  it('còn trang sau thì nút "Tải thêm" phát loadMore', () => {
    const { fixture } = setup({ conversations: [session(1, HOUR)], hasMore: true });
    const component = fixture.componentInstance;
    const loadMore = vi.fn();
    component.loadMore.subscribe(loadMore);

    (fixture.nativeElement as HTMLElement)
      .querySelector<HTMLButtonElement>('.panel-action')!
      .click();

    expect(loadMore).toHaveBeenCalled();
  });
});
