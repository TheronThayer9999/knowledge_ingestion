import { TextFieldModule } from '@angular/cdk/text-field';
import {
  Component,
  DestroyRef,
  ElementRef,
  Injector,
  afterNextRender,
  computed,
  inject,
  signal,
  viewChild,
} from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatDialog } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatSelectModule } from '@angular/material/select';
import { MatTooltipModule } from '@angular/material/tooltip';
import { map, Observable, of, Subscription, switchMap, tap } from 'rxjs';
import { CONVERSATION_PAGE, ConversationService } from '../../core/chat/conversation.service';
import type { ChatChart, ChatCite, Conversation } from '../../core/chat/chat.models';
import { ChatService } from '../../core/chat/chat.service';
import { LangService, type TranslationKey } from '../../core/i18n/lang.service';
import type { Category } from '../../core/knowledge/knowledge.models';
import { KnowledgeService } from '../../core/knowledge/knowledge.service';
import { MessageService } from '../../shared/feedback/message.service';
import { MarkdownPipe } from '../../shared/markdown/markdown.pipe';
import { EchartsChart } from '../../shared/chart/echarts-chart';
import {
  DocumentDetailDialog,
  type DocumentDetailData,
} from '../documents/dialogs/document-detail-dialog';

interface ChatMessage {
  role: 'user' | 'assistant';
  content: string;
  cites?: ChatCite[];
  /**
   * Biểu đồ backend dựng từ chunk đã dẫn — con số nào cũng có nguồn, FE chỉ vẽ.
   * Lượt của user và câu trả lời không có biểu đồ thì để trống.
   */
  charts?: ChatChart[];
  /** Phần model suy nghĩ (chỉ có ở lượt bật suy luận sâu) — hiển thị dạng gập */
  reasoning?: string;
  /**
   * Phạm vi hiệu lực của lượt ĐÃ LƯU (backend ghi per-message). Lượt vừa gõ
   * chưa có nên để trống — mở lại phiên mới thấy dòng phạm vi dưới tin.
   */
  scopeIds?: number[];
}

const SUGGESTIONS: TranslationKey[] = ['CHAT_SUGGEST_1', 'CHAT_SUGGEST_2', 'CHAT_SUGGEST_3'];

const MINUTE_MS = 60_000;
const DAY_MS = 86_400_000;

/**
 * Chat RAG có phiên: mọi lượt gắn vào một `conversation_id` đã lưu ở DB,
 * sidebar liệt kê/tìm lại/đổi tên/xoá phiên.
 *
 * Hai quyết định contract chi phối phần còn lại:
 * - Phiên tạo LAZY (bấm "Phiên mới" chưa tạo row nào) ngay trước lượt gửi đầu
 *   tiên, để `ChatResponse` khỏi phải mang conversation_id (không đụng DTO/swagger).
 * - Đã có phiên thì KHÔNG gửi `history` — server là nguồn lịch sử duy nhất.
 * Vì backend chỉ đi theo phạm vi/model/thinking của phiên khi request để TRỐNG
 * (plan() trong agent/usecase/chat_service.go), không có cách nào nói "bỏ phạm
 * vi" hay "tắt thinking" giữa chừng — nên hai lựa chọn đó bị khoá khi phiên đã
 * có lượt, muốn đổi thì tạo phiên mới.
 */
@Component({
  selector: 'app-chat',
  imports: [
    TextFieldModule,
    MatButtonModule,
    MatIconModule,
    MatFormFieldModule,
    MatSelectModule,
    MatTooltipModule,
    MarkdownPipe,
    EchartsChart,
  ],
  templateUrl: './chat.html',
  styleUrl: './chat.scss',
})
export class Chat {
  protected readonly lang = inject(LangService);
  private readonly chat = inject(ChatService);
  private readonly sessions = inject(ConversationService);
  private readonly knowledge = inject(KnowledgeService);
  private readonly messages = inject(MessageService);
  private readonly dialog = inject(MatDialog);
  private readonly injector = inject(Injector);
  private readonly destroyRef = inject(DestroyRef);

  protected readonly categories = signal<Category[]>([]);
  protected readonly turns = signal<ChatMessage[]>([]);
  protected readonly draft = signal('');
  protected readonly scope = signal(0);
  protected readonly sending = signal(false);
  /** Suy luận sâu: gửi `thinking: true` để model chịu khó nghĩ trước khi đáp */
  protected readonly thinking = signal(false);
  /** Sidebar tiện ích (danh sách phiên + phạm vi + phiên mới) — ẩn để khung chat rộng hơn */
  protected readonly panelOpen = signal(true);
  protected readonly suggestions = SUGGESTIONS;

  protected readonly conversations = signal<Conversation[]>([]);
  /** null = phiên chưa tạo (màn "Bắt đầu cuộc trò chuyện") */
  protected readonly activeId = signal<number | null>(null);
  protected readonly listLoading = signal(false);
  protected readonly listFailed = signal(false);
  protected readonly listHasMore = signal(false);
  protected readonly turnsLoading = signal(false);
  /** Đang đổi tên inline ở mục nào (null là không đổi) */
  protected readonly renamingId = signal<number | null>(null);
  protected readonly renameDraft = signal('');

  /** Request đang bay — giữ subscription để nút Dừng hủy được giữa chừng */
  private pending?: Subscription;

  private readonly scroller = viewChild<ElementRef<HTMLElement>>('scroller');

  /**
   * Phiên đã có lượt thì phạm vi + suy luận sâu chốt theo phiên ( xem comment
   * đầu class) — sidebar và khung chat đều lấy cờ này để khoá điều khiển.
   */
  protected readonly sessionLocked = computed(
    () => this.activeId() !== null && this.turns().length > 0,
  );

  constructor() {
    // Danh mục chỉ phục vụ chọn phạm vi — lỗi thì vẫn chat toàn kho được
    this.knowledge.listCategories().subscribe({
      next: (categories) => this.categories.set(categories),
      error: () => this.categories.set([]),
    });
    this.loadSessions();
    // Rời trang giữa lúc đang chờ câu trả lời thì đừng để request treo
    this.destroyRef.onDestroy(() => this.pending?.unsubscribe());
  }

  protected toggleThinking(): void {
    if (this.sessionLocked()) {
      this.messages.warning('CHAT_LOCKED_HINT');
      return;
    }
    this.thinking.update((value) => !value);
  }

  protected togglePanel(): void {
    this.panelOpen.update((value) => !value);
  }

  protected onInput(event: Event): void {
    this.draft.set((event.target as HTMLTextAreaElement).value);
  }

  protected onScopeChange(value: number): void {
    if (this.sessionLocked()) {
      // mat-select đã disabled khi khoá, nhưng giữ guard cho chắc (bàn phím/IME)
      return;
    }
    this.scope.set(value);
  }

  /** Enter gửi, Shift+Enter xuống dòng; bỏ qua khi IME đang ghép chữ */
  protected onKeydown(event: KeyboardEvent): void {
    if (event.key !== 'Enter' || event.shiftKey || event.isComposing) {
      return;
    }
    event.preventDefault();
    this.send();
  }

  protected send(): void {
    this.ask(this.draft());
  }

  protected askSuggestion(key: TranslationKey): void {
    this.ask(this.lang.t(key));
  }

  // ------------------------------------------------------------- sidebar phiên

  /** Nạp lại trang đầu danh sách phiên (dùng sau khi tạo/đổi tên/xoá) */
  protected loadSessions(): void {
    this.listLoading.set(true);
    this.listFailed.set(false);
    this.sessions.list().subscribe({
      next: (rows) => {
        this.listLoading.set(false);
        this.conversations.set(rows);
        this.listHasMore.set(rows.length >= CONVERSATION_PAGE);
      },
      error: () => {
        this.listLoading.set(false);
        this.listFailed.set(true);
      },
    });
  }

  /** Cursor keyset: id của mục cuối đang hiển thị — không phải offset */
  protected loadMoreSessions(): void {
    const cursor = this.conversations()[this.conversations().length - 1]?.id;
    if (!cursor || this.listLoading()) {
      return;
    }
    this.listLoading.set(true);
    this.sessions.list(cursor).subscribe({
      next: (rows) => {
        this.listLoading.set(false);
        this.conversations.update((list) => [...list, ...rows]);
        this.listHasMore.set(rows.length >= CONVERSATION_PAGE);
      },
      error: () => {
        this.listLoading.set(false);
        this.listFailed.set(true);
      },
    });
  }

  /** "Phiên mới" chỉ về trạng thái rỗng — row chưa được tạo (lazy, xem đầu class) */
  protected startNewSession(): void {
    if (this.sending()) {
      this.messages.warning('CHAT_BUSY_HINT');
      return;
    }
    this.activeId.set(null);
    this.turns.set([]);
    this.renamingId.set(null);
    this.thinking.set(false);
    this.scope.set(0);
  }

  protected openSession(conversation: Conversation): void {
    // Khoá khi đang trả lời: đổi phiên giữa chừng là mất lượt đang bay
    if (this.sending() || this.activeId() === conversation.id) {
      return;
    }
    this.renamingId.set(null);
    this.activeId.set(conversation.id);
    this.applySessionChoices(conversation);
    this.turnsLoading.set(true);
    this.turns.set([]);
    this.sessions.messages(conversation.id).subscribe({
      next: (rows) => {
        this.turnsLoading.set(false);
        this.turns.set(
          rows.map((row) => ({
            role: row.role,
            content: row.content,
            cites: row.cites,
            charts: row.charts,
            // reasoning không được lưu (không có cột) — mở lại phiên chỉ còn đáp án
            scopeIds: row.categoryIds,
          })),
        );
        this.scrollToBottom();
      },
      error: (error: unknown) => {
        this.turnsLoading.set(false);
        // Phiên của người khác/đã bị xoá đều là 404: báo rồi về màn phiên mới
        this.messages.errorFromApi(error, 'AUTH_ERROR_GENERIC');
        this.startNewSession();
        this.loadSessions();
      },
    });
  }

  /** Phiên nhớ lựa chọn của nó — mở lại là khôi phục phạm vi + thinking */
  private applySessionChoices(conversation: Conversation): void {
    this.thinking.set(conversation.thinking);
    this.scope.set(conversation.categoryIds.length === 1 ? conversation.categoryIds[0] : 0);
  }

  protected beginRename(conversation: Conversation): void {
    if (this.sending()) {
      this.messages.warning('CHAT_BUSY_HINT');
      return;
    }
    this.renamingId.set(conversation.id);
    this.renameDraft.set(conversation.title);
  }

  protected onRenameInput(event: Event): void {
    this.renameDraft.set((event.target as HTMLInputElement).value);
  }

  protected onRenameKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault();
      this.cancelRename();
      return;
    }
    if (event.key === 'Enter') {
      event.preventDefault();
      this.saveRename();
    }
  }

  protected cancelRename(): void {
    this.renamingId.set(null);
  }

  protected saveRename(): void {
    const id = this.renamingId();
    const title = this.renameDraft().trim();
    if (id === null) {
      return;
    }
    this.renamingId.set(null);
    const current = this.conversations().find((item) => item.id === id);
    // Không đổi gì hoặc rỗng thì khỏi gọi PATCH (backend bound min=1 nên rỗng là 400)
    if (title === '' || title === current?.title) {
      return;
    }
    this.sessions.rename(id, title).subscribe({
      next: (updated) => this.upsertSession(updated),
      error: (error: unknown) => this.messages.errorFromApi(error, 'AUTH_ERROR_GENERIC'),
    });
  }

  protected async confirmDelete(conversation: Conversation): Promise<void> {
    if (this.sending()) {
      this.messages.warning('CHAT_BUSY_HINT');
      return;
    }
    const ok = await this.messages.confirm('CHAT_DELETE_TITLE', 'CHAT_DELETE_BODY');
    if (!ok) {
      return;
    }
    this.sessions.remove(conversation.id).subscribe({
      next: () => {
        this.conversations.update((list) => list.filter((item) => item.id !== conversation.id));
        if (this.activeId() === conversation.id) {
          this.startNewSession();
        }
        this.messages.success('CHAT_DELETED');
      },
      error: (error: unknown) => this.messages.errorFromApi(error, 'AUTH_ERROR_GENERIC'),
    });
  }

  protected isSessionActive(conversation: Conversation): boolean {
    return this.activeId() === conversation.id;
  }

  /** Mốc thời gian tương đối cho sidebar; quá 7 ngày thì in ngày tuyệt đối */
  protected sessionTime(conversation: Conversation): string {
    const then = conversation.lastMessageAt.getTime();
    const mins = Math.max(0, Math.round((Date.now() - then) / MINUTE_MS));
    if (mins < 1) {
      return this.lang.t('CHAT_TIME_NOW');
    }
    if (mins < 60) {
      return this.withCount('CHAT_TIME_MIN', mins);
    }
    if (mins < 60 * 24) {
      return this.withCount('CHAT_TIME_HOUR', Math.round(mins / 60));
    }
    if (mins < 60 * 24 * 7) {
      return this.withCount('CHAT_TIME_DAY', Math.round((Date.now() - then) / DAY_MS));
    }
    return this.formatDate(conversation.lastMessageAt);
  }

  // --------------------------------------------------------------- transcript

  /**
   * Nhóm tin cùng người gửi: tin liền trước cùng role thì siết khoảng cách
   * lại (kiểu iMessage/Gmail) — đọc thấy rõ cụm của một người.
   */
  protected isGrouped(index: number): boolean {
    const turns = this.turns();
    return index > 0 && turns[index - 1]?.role === turns[index]?.role;
  }

  /** Tên thư mục của lượt đã lưu — id lạ (thư mục bị xoá sau đó) in nguyên id */
  protected categoryNames(ids: number[]): string {
    const catalog = this.categories();
    return ids
      .map((id) => catalog.find((category) => category.id === id)?.name ?? `#${id}`)
      .join(', ');
  }

  /**
   * Bấm nguồn trích dẫn: tải metadata bài theo article_id (backend không
   * gửi kèm trong cites) rồi mở dialog chi tiết như trong trang Tài liệu.
   */
  protected openCite(cite: ChatCite): void {
    this.knowledge.getArticle(cite.articleId).subscribe({
      next: (article) => {
        const categoryName =
          this.categories().find((category) => category.id === article.categoryId)?.name ?? '';
        this.dialog.open<DocumentDetailDialog, DocumentDetailData>(DocumentDetailDialog, {
          data: { article, categoryName },
          maxWidth: '94vw',
          autoFocus: 'dialog',
        });
      },
      // Bài đã bị xóa từ lúc trả lời — báo lỗi nhẹ, không chặn hội thoại
      error: (error: unknown) => this.messages.errorFromApi(error, 'AUTH_ERROR_GENERIC'),
    });
  }

  /**
   * Bấm chip `[^n]` trong đáp án: chip do MarkdownPipe sinh ra (không phải phần tử
   * Angular) nên bắt sự kiện kiểu delegate từ .markdown-body, số nguồn đọc từ
   * textContent vì id/data-* nằm ngoài allowlist sanitizer.
   */
  protected onCiteRefClick(event: MouseEvent, turnIndex: number): void {
    this.jumpToCite(event.target, turnIndex);
  }

  /** Chip là `sup[role=button]` chứ không phải `<button>` nên bàn phím phải tự lo */
  protected onCiteRefKey(event: KeyboardEvent, turnIndex: number): void {
    if (event.key !== 'Enter' && event.key !== ' ') {
      return;
    }
    event.preventDefault();
    this.jumpToCite(event.target, turnIndex);
  }

  private jumpToCite(target: EventTarget | null, turnIndex: number): void {
    const chip = target instanceof Element ? target.closest('.md-cite-ref') : null;
    const number = Number((chip?.textContent ?? '').trim());
    if (!chip || !Number.isInteger(number) || number < 1) {
      return;
    }
    this.scrollToCite(turnIndex, number);
  }

  /**
   * Bấm chip nguồn dưới biểu đồ: cùng một bộ đếm với chip `[^n]` trong đáp án
   * nên đi chung đường nhảy — biểu đồ không tự mở dialog tài liệu, nó chỉ dẫn
   * xuống khối nguồn để người đọc bấm tiếp vào bài.
   */
  protected onChartCite(number: number, turnIndex: number): void {
    this.scrollToCite(turnIndex, number);
  }

  /** Mở khối nguồn (mặc định gập) rồi nhảy tới mục thứ `number` trong cùng bong bóng */
  private scrollToCite(turnIndex: number, number: number): void {
    const bubble = this.bubbleAt(turnIndex);
    if (!bubble) {
      return;
    }
    const panel = bubble.querySelector<HTMLDetailsElement>('details.cites');
    if (panel) {
      panel.open = true;
    }
    const item = this.citeItem(bubble, number);
    if (!item) {
      return;
    }
    item.scrollIntoView({ block: 'nearest' });
    this.flash(item);
  }

  /** .turn thứ index — tra từ DOM chứ không giữ reference vì danh sách thêm lượt liên tục */
  private bubbleAt(turnIndex: number): HTMLElement | null {
    const turn = this.scroller()?.nativeElement.querySelectorAll<HTMLElement>('.turn')[turnIndex];
    return turn?.querySelector<HTMLElement>('.bubble') ?? null;
  }

  /** Khớp số nguồn bằng chính chữ in trên chip: danh sách và marker cùng một bộ đếm */
  private citeItem(bubble: HTMLElement, number: number): HTMLElement | null {
    const match = Array.from(bubble.querySelectorAll<HTMLElement>('.cite-n')).find(
      (element) => element.textContent?.trim() === String(number),
    );
    return match?.closest<HTMLElement>('li') ?? null;
  }

  private flash(item: HTMLElement): void {
    item.classList.remove('is-flash');
    // Đọc size ép browser tính lại layout, nếu không bấm liền 2 lần cùng một
    // nguồn thì animation không chạy lại
    void item.offsetWidth;
    item.classList.add('is-flash');
    item.addEventListener('animationend', () => item.classList.remove('is-flash'), {
      once: true,
    });
  }

  /**
   * Dừng lúc model đang trả lời: hủy request, bỏ lượt user vừa ghi vào
   * transcript và trả câu hỏi về ô soạn — chưa có đáp án nên giữ nó trong
   * history chỉ tổ làm nhiễu ngữ cảnh lượt sau.
   */
  protected stop(): void {
    this.pending?.unsubscribe();
    this.pending = undefined;
    this.sending.set(false);
    this.dropLastUserTurn();
    this.refreshSidebar();
  }

  private ask(text: string): void {
    const question = text.trim();
    if (question === '' || this.sending()) {
      return;
    }
    this.sending.set(true);
    this.draft.set('');
    this.pending = this.resolveSession()
      .pipe(
        tap(() => {
          this.turns.update((list) => [...list, { role: 'user', content: question }]);
          this.scrollToBottom();
        }),
        switchMap((sessionId) =>
          this.chat.send({
            message: question,
            conversationId: sessionId,
            categoryId: this.scope() || null,
            thinking: this.thinking(),
          }),
        ),
      )
      .subscribe({
        next: (answer) => {
          this.pending = undefined;
          this.turns.update((list) => [
            ...list,
            {
              role: 'assistant',
              content: answer.answer,
              cites: answer.cites,
              charts: answer.charts,
              reasoning: answer.reasoning,
            },
          ]);
          this.sending.set(false);
          this.scrollToBottom();
          // Backend điền title từ câu hỏi đầu + dời mốc last_message_at: sidebar
          // phải nạp lại mới thấy tên và thứ tự mới của phiên vừa tạo
          this.refreshSidebar();
        },
        error: (error: unknown) => {
          this.pending = undefined;
          this.sending.set(false);
          this.recoverQuestion(question);
          this.messages.errorFromApi(error, 'AUTH_ERROR_GENERIC');
          this.scrollToBottom();
        },
      });
  }

  /**
   * Id phiên cho lượt sắp gửi: đang có phiên thì dùng lại, chưa có thì tạo
   * NGAY LÚC NÀY (lazy) với phạm vi + thinking đang chọn.
   */
  private resolveSession(): Observable<number> {
    const existing = this.activeId();
    if (existing) {
      return of(existing);
    }
    return this.sessions
      .create({
        categoryIds: this.scope() ? [this.scope()] : [],
        thinking: this.thinking(),
      })
      .pipe(
        tap((conversation) => {
          this.activeId.set(conversation.id);
          this.upsertSession(conversation);
        }),
        map((conversation) => conversation.id),
      );
  }

  /** Đưa một phiên lên đầu sidebar (ghi đè nếu id đã có) */
  private upsertSession(conversation: Conversation): void {
    this.conversations.update((list) => {
      const others = list.filter((item) => item.id !== conversation.id);
      return [conversation, ...others];
    });
  }

  /**
   * Sau lượt đầu/lượt bị dừng: chỉ nạp lại trang 1 danh sách khi sidebar đang
   * mở, để không gọi request vô ích khi người dùng đang gập panel.
   */
  private refreshSidebar(): void {
    if (this.panelOpen()) {
      this.loadSessions();
    }
  }

  /** Lỗi giữa lúc gửi: chưa có đáp án nên lượt user không ở trong DB — gỡ về ô soạn */
  private recoverQuestion(question: string): void {
    const popped = this.dropLastUserTurn();
    if (!popped && this.draft().trim() === '') {
      this.draft.set(question);
    }
  }

  private dropLastUserTurn(): boolean {
    const turns = this.turns();
    const last = turns[turns.length - 1];
    if (last?.role !== 'user') {
      return false;
    }
    this.turns.set(turns.slice(0, -1));
    this.draft.set(last.content);
    return true;
  }

  /** "3 lượt" — đếm message của phiên trong sidebar */
  protected turnCountLabel(count: number): string {
    return this.withCount('CHAT_SESSION_TURNS', count);
  }

  private withCount(key: TranslationKey, n: number): string {
    // i18n của app không có cơ chế nội suy, nên thay {n} tại chỗ dùng
    return this.lang.t(key).replace('{n}', String(n));
  }

  private formatDate(date: Date): string {
    const pad = (value: number): string => String(value).padStart(2, '0');
    return `${pad(date.getDate())}/${pad(date.getMonth() + 1)}/${date.getFullYear()}`;
  }

  private scrollToBottom(): void {
    afterNextRender(
      () => {
        const element = this.scroller()?.nativeElement;
        element?.scrollTo({ top: element.scrollHeight, behavior: 'smooth' });
      },
      { injector: this.injector },
    );
  }
}
