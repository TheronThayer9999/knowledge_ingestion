import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  ElementRef,
  Injector,
  afterNextRender,
  computed,
  effect,
  inject,
  input,
  output,
  signal,
  viewChild,
} from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatMenuModule } from '@angular/material/menu';
import { MatTooltipModule } from '@angular/material/tooltip';
import type { Conversation } from '../../core/chat/chat.models';
import { LangService, type TranslationKey } from '../../core/i18n/lang.service';
import {
  groupSessions,
  messageCountLabel,
  sessionTitle,
  timeAgo,
  type SessionGroup,
} from './session-time';

/** Nhịp làm tươi nhãn "5 phút trước" — 60s là đủ, không cần tick từng giây */
const CLOCK_TICK_MS = 60_000;

/**
 * Danh sách phiên trong sidebar (Đợt 27). Component chỉ TRÌNH BÀY: mọi thay đổi
 * thật (mở phiên, PATCH title, DELETE) phát ra output để trang `Chat` gọi service,
 * nên logic phiên vẫn nằm một chỗ và component này test được không cần HTTP.
 *
 * Nhánh logic khó đều ở `session-time.ts` (chia nhóm ngày, timeAgo) — giữ đây
 * càng ít số học càng tốt.
 */
@Component({
  selector: 'app-chat-sessions',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [MatButtonModule, MatIconModule, MatMenuModule, MatTooltipModule],
  templateUrl: './chat-sessions.html',
  styleUrl: './chat-sessions.scss',
})
export class ChatSessions {
  protected readonly lang = inject(LangService);
  private readonly destroyRef = inject(DestroyRef);
  private readonly injector = inject(Injector);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);

  readonly conversations = input.required<Conversation[]>();
  /** Phiên đang mở trong khung chat — null là chưa tạo phiên nào */
  readonly activeId = input<number | null>(null);
  /** Đang chờ đáp án: khoá sửa/xoá và gắn chấm trạng thái vào hàng đang xử lý */
  readonly answering = input(false);
  readonly loading = input(false);
  readonly failed = input(false);
  readonly hasMore = input(false);
  /** Câu hỏi mẫu cho empty state — trang đưa, component không tự bịa nội dung */
  readonly suggestions = input<TranslationKey[]>([]);

  readonly openSession = output<Conversation>();
  readonly rename = output<{ id: number; title: string }>();
  readonly remove = output<Conversation>();
  readonly loadMore = output<void>();
  readonly retry = output<void>();
  readonly askExample = output<TranslationKey>();

  protected readonly renamingId = signal<number | null>(null);
  protected readonly renameDraft = signal('');
  private readonly clock = signal(Date.now());
  private readonly renameInput = viewChild<ElementRef<HTMLInputElement>>('renameInput');

  protected readonly groups = computed<SessionGroup[]>(() =>
    groupSessions(this.conversations(), new Date(this.clock())),
  );

  constructor() {
    // Đổi phiên thì huỷ ô đổi tên đang mở — để lại sẽ thành ô nhập treo ở hàng khác
    effect(() => {
      this.activeId();
      this.renamingId.set(null);
    });
    // Con trỏ ô nhập: vào rename là gõ được ngay, không phải tab thêm lần nữa
    effect(() => {
      if (this.renamingId() === null) {
        return;
      }
      const element = this.renameInput()?.nativeElement;
      afterNextRender(
        () => {
          element?.focus();
          element?.select();
        },
        { injector: this.injector },
      );
    });
    const timer = setInterval(() => this.clock.set(Date.now()), CLOCK_TICK_MS);
    this.destroyRef.onDestroy(() => clearInterval(timer));
  }

  protected isActive(conversation: Conversation): boolean {
    return this.activeId() === conversation.id;
  }

  /** Chấm trạng thái chỉ gắn lên phiên đang bị giữ chờ, không gắn cả list */
  protected isProcessing(conversation: Conversation): boolean {
    return this.answering() && this.isActive(conversation);
  }

  protected titleOf(conversation: Conversation): string {
    return sessionTitle(conversation, (key) => this.lang.t(key));
  }

  protected timeOf(conversation: Conversation): string {
    return timeAgo(conversation.lastMessageAt, new Date(this.clock()), (key) => this.lang.t(key));
  }

  protected countOf(conversation: Conversation): string {
    return messageCountLabel(conversation.messageCount, (key) => this.lang.t(key));
  }

  protected beginRename(conversation: Conversation): void {
    if (this.answering()) {
      return;
    }
    this.renamingId.set(conversation.id);
    this.renameDraft.set(conversation.title);
  }

  protected onRenameInput(event: Event): void {
    this.renameDraft.set((event.target as HTMLInputElement).value);
  }

  /** Enter lưu, Esc hủy — đúng điều khoản "đổi tên inline bằng bàn phím" */
  protected onRenameKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault();
      this.cancelRename();
      return;
    }
    if (event.key === 'Enter') {
      event.preventDefault();
      this.commitRename();
    }
  }

  private cancelRename(): void {
    const id = this.renamingId();
    this.renamingId.set(null);
    this.renameDraft.set('');
    // Trả focus về hàng vừa sửa để Tab tiếp không rơi xuống cuối trang
    this.focusRow(id);
  }

  private commitRename(): void {
    const id = this.renamingId();
    const title = this.renameDraft().trim();
    if (id === null) {
      return;
    }
    this.renamingId.set(null);
    this.renameDraft.set('');
    this.focusRow(id);
    const current = this.conversations().find((item) => item.id === id);
    // Không đổi gì thì khỏi phát output — rỗng thì backend chặn 400 ngay
    if (title === '' || title === current?.title) {
      return;
    }
    this.rename.emit({ id, title });
  }

  /** Focus lại hàng sau khi đóng ô đổi tên — Esc không được ném focus về cuối trang */
  private focusRow(id: number | null): void {
    if (id === null) {
      return;
    }
    afterNextRender(
      () => {
        const row = this.host.nativeElement.querySelector<HTMLElement>(
          `[data-session-id="${id}"] .session-main`,
        );
        row?.focus();
      },
      { injector: this.injector },
    );
  }
}
