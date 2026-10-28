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
import { MatIconModule } from '@angular/material/icon';
import { MatTooltipModule } from '@angular/material/tooltip';
import { map, Observable, of, Subscription, switchMap, tap } from 'rxjs';
import { CONVERSATION_PAGE, ConversationService } from '../../core/chat/conversation.service';
import type { ChatChart, ChatCite, Conversation } from '../../core/chat/chat.models';
import { clipSessionTitle } from '../../core/chat/chat.models';
import { ChatService } from '../../core/chat/chat.service';
import { LangService, type TranslationKey } from '../../core/i18n/lang.service';
import type { Category } from '../../core/knowledge/knowledge.models';
import { KnowledgeService } from '../../core/knowledge/knowledge.service';
import type { LlmProviderOption } from '../../core/llm/llm.models';
import { LlmService } from '../../core/llm/llm.service';
import { MessageService } from '../../shared/feedback/message.service';
import { MarkdownPipe } from '../../shared/markdown/markdown.pipe';
import { createStreamReveal } from '../../shared/markdown/stream-reveal';
import { EchartsChart } from '../../shared/chart/echarts-chart';
import { ChatContextChips, type ModelChoice } from './chat-context-chips';
import { ChatSessions } from './chat-sessions';
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
  /**
   * Provider/model THỰC đã trả lời lượt này (Đợt 28) — vẽ badge dưới đáp án.
   * Lượt mới lấy từ response, lượt mở lại từ cột đã lưu trong DB.
   */
  provider?: string;
  model?: string;
}

const SUGGESTIONS: TranslationKey[] = ['CHAT_SUGGEST_1', 'CHAT_SUGGEST_2', 'CHAT_SUGGEST_3'];

/** Nhịp hé lộ đáp án ~30fps — đủ mượt cho "đang gõ", đủ thưa để marked không phải parse 60 lần/giây */
const REVEAL_INTERVAL_MS = 33;

/**
 * Chat RAG có phiên: mọi lượt gắn vào một `conversation_id` đã lưu ở DB,
 * sidebar liệt kê/tìm lại/đổi tên/xoá phiên (danh sách ở `chat-sessions.ts`,
 * chip phạm vi/suy luận sâu ở `chat-context-chips.ts`).
 *
 * Hai quyết định contract chi phối phần còn lại:
 * - Phiên tạo LAZY (bấm "Phiên mới" chưa tạo row nào) ngay trước lượt gửi đầu
 *   tiên, để `ChatResponse` khỏi phải mang conversation_id (không đụng DTO/swagger).
 * - Đã có phiên thì KHÔNG gửi `history` — server là nguồn lịch sử duy nhất.
 * Vì backend chỉ đi theo phạm vi/model/thinking của phiên khi request để TRỐNG
 * (plan() trong agent/usecase/chat_service.go), không có cách nào nói "bỏ phạm
 * vi" hay "tắt thinking" giữa chừng — nên hai lựa chọn đó bị khoá khi phiên đã
 * có lượt, muốn đổi thì tạo phiên mới.
 *
 * Sidebar CẬP NHẬT TRƯỚC (optimistic): bump số tin nhắn + mốc thời gian
 * ngay lúc bấm gửi, không đợi đáp án; `refreshSidebar()` sau lượt xong vẫn là
 * nguồn thật chốt lại, nên lệch tạm thời không bao giờ đi vào DB.
 */
@Component({
  selector: 'app-chat',
  imports: [
    TextFieldModule,
    MatButtonModule,
    MatIconModule,
    MatTooltipModule,
    MarkdownPipe,
    EchartsChart,
    ChatSessions,
    ChatContextChips,
  ],
  templateUrl: './chat.html',
  styleUrl: './chat.scss',
  // Esc đóng drawer ở màn hẹp. Nghe ở document chứ không trên <aside>: mở drawer
  // bằng nút toggle thì focus vẫn nằm trên nút (ngoài drawer), nên listener gắn
  // trong aside sẽ không bao giờ nhận phím.
  host: { '(document:keydown.escape)': 'onEscape($event)' },
})
export class Chat {
  protected readonly lang = inject(LangService);
  private readonly chat = inject(ChatService);
  private readonly sessions = inject(ConversationService);
  private readonly knowledge = inject(KnowledgeService);
  private readonly llm = inject(LlmService);
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
  /**
   * Provider/model đang chọn cho lượt kế tiếp (Đợt 28). Rỗng = theo cấu hình
   * mặc định — đó là lựa chọn hợp lệ, KHÁC với "chưa biết": cần phân biệt
   * "chưa chọn" và "chọn theo cấu hình" thì mới giữ được lựa chọn khi mở phiên.
   */
  protected readonly provider = signal('');
  protected readonly model = signal('');
  /**
   * Provider sẵn sàng để chọn (backend đã dựng agent). Rỗng = chưa tải được
   * options → ẩn chip mô hình, chat vẫn chạy bằng mặc định cấu hình.
   */
  protected readonly providers = signal<LlmProviderOption[]>([]);
  /**
   * Sidebar tiện ích (danh sách phiên + phạm vi + phiên mới) — ẩn để khung chat
   * rộng hơn. Màn hẹp thì mặc định ĐÓNG: drawer phủ + scrim sẽ che mất khung chat
   * ngay lần mở trang nếu để true.
   */
  protected readonly panelOpen = signal(!this.isNarrowViewport());
  protected readonly suggestions = SUGGESTIONS;

  protected readonly conversations = signal<Conversation[]>([]);
  /** null = phiên chưa tạo (màn "Bắt đầu cuộc trò chuyện") */
  protected readonly activeId = signal<number | null>(null);
  protected readonly listLoading = signal(false);
  protected readonly listFailed = signal(false);
  protected readonly listHasMore = signal(false);
  protected readonly turnsLoading = signal(false);

  /** Request đang bay — giữ subscription để nút Dừng hủy được giữa chừng */
  private pending?: Subscription;
  /**
   * Chữ đang hé lộ của đáp án cuối; null = không hoạt cảnh, in nguyên văn.
   * Nguồn dữ liệu VẪN là `turns` (đáp án đầy đủ ngay từ đầu), nên chip nguồn
   * và hành vi trích dẫn không hề đổi — chỉ mặt chữ hiện dần (Đợt 27).
   */
  protected readonly revealText = signal<string | null>(null);
  private revealTimer?: ReturnType<typeof setInterval>;

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
    this.loadProviders();
    this.loadSessions();
    // Rời trang giữa lúc đang chờ câu trả lời thì đừng để request treo
    this.destroyRef.onDestroy(() => {
      this.pending?.unsubscribe();
      this.stopReveal();
    });
  }

  /**
   * Nạp danh sách provider để chip mô hình có gì để chọn. Lỗi thì im lặng:
   * backend có thể cũ (chưa có endpoint) hoặc token hết hạn — chip ẩn đi là
   * đủ, báo toast làm phiền người đang chat.
   */
  private loadProviders(): void {
    this.llm.options().subscribe({
      next: (options) => {
        // Provider chưa dựng được agent (thiếu key) không chọn được ở chat —
        // admin vẫn thấy trong Settings để biết cấu hình sai ở đâu.
        this.providers.set(options.providers.filter((item) => item.available));
      },
      error: () => this.providers.set([]),
    });
  }

  /**
   * Chọn provider/model cho lượt kế tiếp. KHÔNG khoá theo phiên: backend nhận
   * provider/model mỗi lượt và lưu per-message nên đổi giữa hội thoại hợp lệ.
   */
  protected selectModel(choice: ModelChoice): void {
    this.provider.set(choice.provider);
    this.model.set(choice.model);
  }

  /** Nhãn hiển thị của provider — rơi về slug khi không có trong danh sách options */
  protected providerLabel(slug: string): string {
    return this.providers().find((item) => item.slug === slug)?.label ?? slug;
  }

  /** Badge dưới đáp án: `provider · model` — rỗng khi lượt không có thông tin */
  protected modelBadge(turn: ChatMessage): string {
    if (!turn.provider) {
      return '';
    }
    const label = this.providerLabel(turn.provider);
    return turn.model ? `${label} · ${turn.model}` : label;
  }

  protected toggleThinking(): void {
    // Chip KHÔNG bị disabled thật (để còn đọc được tooltip), nên chốt ở đây
    if (this.sending()) {
      this.messages.warning('CHAT_BUSY_HINT');
      return;
    }
    if (this.sessionLocked()) {
      this.messages.warning('CHAT_LOCKED_HINT');
      return;
    }
    this.thinking.update((value) => !value);
  }

  /** Chip phạm vi đã tự chặn khi phiên khoá/đang bận nên tới đây là được đổi */
  protected selectScope(id: number): void {
    this.scope.set(id);
  }

  /** Nhãn cho chip phạm vi: 0 là toàn bộ kho, id lạ (thư mục bị xoá) in nguyên id */
  protected readonly scopeLabel = computed(() =>
    this.scope() === 0 ? this.lang.t('CHAT_SCOPE_ALL') : this.categoryNames([this.scope()]),
  );

  protected togglePanel(): void {
    this.panelOpen.update((value) => !value);
  }

  /** Scrim/Esc bên drawer đóng sidebar — chỉ có nghĩa khi đang mở */
  protected closePanel(): void {
    if (this.panelOpen()) {
      this.panelOpen.set(false);
    }
  }

  /**
   * Esc chỉ đóng DRAWER (<768px) — desktop panel nằm trong luồng, đóng ngang
   * hứng phím Esc của người dùng đang gõ dở. Bỏ qua khi Esc đang đóng menu/dialog
   * (overlay tự xử lý lớp của nó trước, không được đóng luôn thứ phía sau).
   */
  protected onEscape(event: Event): void {
    if (!this.panelOpen() || !this.isNarrowViewport()) {
      return;
    }
    if (event.target instanceof Element && event.target.closest('.cdk-overlay-container')) {
      return;
    }
    this.closePanel();
  }

  /** drawer overlay chỉ tồn tại dưới 768px — khớp đúng @media trong chat.scss */
  private isNarrowViewport(): boolean {
    return window.matchMedia?.('(max-width: 767px)').matches === true;
  }

  protected onInput(event: Event): void {
    this.draft.set((event.target as HTMLTextAreaElement).value);
  }

  /** Enter gửi, Shift+Enter xuống dòng; bỏ qua khi IME đang ghép chữ */
  protected onKeydown(event: KeyboardEvent): void {
    if (event.key !== 'Enter' || event.shiftKey || event.isComposing) {
      return;
    }
    event.preventDefault();
    // Khoá MỀM: ô nhập vẫn gõ được giữa lúc chờ đáp án, Enter chỉ nhắc chứ không gửi
    if (this.sending()) {
      this.messages.warning('CHAT_WAIT_REPLY_HINT');
      return;
    }
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
    this.stopReveal();
    this.thinking.set(false);
    this.scope.set(0);
  }

  protected openSession(conversation: Conversation): void {
    // Khoá khi đang trả lời: đổi phiên giữa chừng là mất lượt đang bay
    if (this.sending() || this.activeId() === conversation.id) {
      return;
    }
    this.activeId.set(conversation.id);
    this.applySessionChoices(conversation);
    // Màn hẹp: chọn phiên xong thì đóng drawer để thấy ngay hội thoại vừa mở;
    // desktop thì panel nằm trong luồng, đóng lại là mất chỗ chọn phiên khác.
    if (this.isNarrowViewport()) {
      this.closePanel();
    }
    this.turnsLoading.set(true);
    this.turns.set([]);
    // Mở lại phiên cũ: đáp án đã nằm nguyên văn trong DB, KHÔNG hoạt cảnh lại
    this.stopReveal();
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
            // provider/model đã lưu per-message — badge dưới đáp án khôi phục
            // đúng, không đoán theo lựa chọn hiện tại
            provider: row.provider,
            model: row.model,
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

  /** Phiên nhớ lựa chọn của nó — mở lại là khôi phục phạm vi + thinking + model */
  private applySessionChoices(conversation: Conversation): void {
    this.thinking.set(conversation.thinking);
    this.scope.set(conversation.categoryIds.length === 1 ? conversation.categoryIds[0] : 0);
    // Phiên lưu provider/model rỗng nghĩa là "theo cấu hình" — đặt rỗng để
    // chip hiện đúng nhãn mặc định, không giữ lựa chọn của phiên trước.
    this.provider.set(conversation.provider);
    this.model.set(conversation.model);
  }

  /**
   * Đổi tên inline mà sidebar phát lên (ô nhập + Enter lưu/Esc hủy nằm trong
   * `chat-sessions.ts`). Rỗng hoặc không đổi đã bị component con chặn ở đó.
   */
  protected onRename(request: { id: number; title: string }): void {
    this.sessions.rename(request.id, request.title).subscribe({
      next: (updated) => this.upsertSession(updated),
      error: (error: unknown) => this.messages.errorFromApi(error, 'AUTH_ERROR_GENERIC'),
    });
  }

  /** Xoá luôn qua hộp xác nhận — không có đường nào xoá im lặng từ menu ⋯ */
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
    this.stopReveal();
    // Lượt user bị gỡ khỏi transcript thì số đếm trên sidebar cũng phải lùi lại
    this.bumpSession(this.activeId(), -1);
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
    this.stopReveal();
    this.pending = this.resolveSession(question)
      .pipe(
        tap(() => {
          this.turns.update((list) => [...list, { role: 'user', content: question }]);
          // Nhóm 2: đếm tin nhắn + mốc hoạt động nhảy tại chỗ ngay lúc bấm gửi
          this.bumpSession(this.activeId(), 1);
          this.scrollToBottom();
        }),
        switchMap((sessionId) =>
          this.chat.send({
            message: question,
            conversationId: sessionId,
            categoryId: this.scope() || null,
            thinking: this.thinking(),
            // Rỗng = theo cấu hình, nên để trống cho backend tự resolve
            provider: this.provider() || undefined,
            model: this.model() || undefined,
          }),
        ),
      )
      .subscribe({
        next: (answer) => {
          this.pending = undefined;
          this.bumpSession(this.activeId(), 1);
          this.turns.update((list) => [
            ...list,
            {
              role: 'assistant',
              content: answer.answer,
              cites: answer.cites,
              charts: answer.charts,
              reasoning: answer.reasoning,
              // Backend trả provider/model ĐÃ resolve — badge dưới đáp án nói
              // đúng model nào thực sự chạy, không đoán theo lựa chọn trên chip.
              provider: answer.provider,
              model: answer.model,
            },
          ]);
          this.sending.set(false);
          this.startReveal(answer.answer);
          this.scrollToBottom();
          // Backend vẫn có thể điền title/dời mốc: sidebar nạp lại để chốt số
          // liệu thật sau khi hoạt cảnh đã bắt đầu (optimistic ở trên là hiển thị)
          this.refreshSidebar();
        },
        error: (error: unknown) => {
          this.pending = undefined;
          this.sending.set(false);
          this.bumpSession(this.activeId(), -1);
          this.recoverQuestion(question);
          this.messages.errorFromApi(error, 'AUTH_ERROR_GENERIC');
          this.scrollToBottom();
        },
      });
  }

  /**
   * Id phiên cho lượt sắp gửi: đang có phiên thì dùng lại, chưa có thì tạo
   * NGAY LÚC NÀY (lazy) với phạm vi + thinking + model đang chọn.
   */
  private resolveSession(question: string): Observable<number> {
    const existing = this.activeId();
    if (existing) {
      return of(existing);
    }
    return this.sessions
      .create({
        // Nhóm 1: FE tự đặt title ngay câu đầu nên sidebar có tên lập tức, không
        // chờ nạp lại. Backend chỉ điền title khi còn rỗng (SetTitleIfEmpty) nên
        // gửi trước không xung đột, cũng không cần thêm endpoint nào.
        title: clipSessionTitle(question),
        categoryIds: this.scope() ? [this.scope()] : [],
        thinking: this.thinking(),
        provider: this.provider() || undefined,
        model: this.model() || undefined,
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
   * Bump cập nhật trước (optimistic) số tin nhắn của một phiên tại chỗ. Không cần
   * dời chỗ: `groupSessions` đã sắp theo `lastMessageAt` nên hàng vừa gõ tự nổi lên đầu.
   */
  private bumpSession(id: number | null, delta: number): void {
    if (id === null) {
      return;
    }
    this.conversations.update((list) =>
      list.map((item) =>
        item.id === id
          ? {
              ...item,
              messageCount: Math.max(0, item.messageCount + delta),
              lastMessageAt: new Date(),
            }
          : item,
      ),
    );
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
    // Ô nhập không khoá cứng nữa (nhóm 8): người dùng có thể đã gõ dở câu kế tiếp
    // trong lúc chờ — nối câu bị gỡ xuống đầu, giữ nguyên chữ họ đã gõ thêm.
    const typed = this.draft();
    this.draft.set(typed.trim() === '' ? last.content : `${last.content}\n\n${typed}`);
    return true;
  }

  // ------------------------------------------------------------------ hoạt cảnh

  /**
   * Hé lộ đáp án từ từ cho giống "đang gõ". Chỉ là mặt chữ: `turns` đã cầm đáp án
   * đầy đủ ngay từ đầu, nên chip nguồn, biểu đồ và mọi hành vi trích dẫn KHÔNG
   * đổi. `splitRevealChunks` bảo đảm không bao giờ cắt nửa marker `[^n]`.
   */
  private startReveal(text: string): void {
    const reveal = this.prefersReducedMotion() ? null : createStreamReveal(text);
    if (reveal === null) {
      this.revealText.set(null);
      return;
    }
    this.revealText.set('');
    this.revealTimer = setInterval(() => {
      this.revealText.set(reveal.step());
      if (reveal.isComplete) {
        this.stopReveal();
        return;
      }
      // Theo sát dòng chữ mới mà không mượt từng pixel — mượt 30fps đã đủ đọc
      this.scrollToBottom('auto');
    }, REVEAL_INTERVAL_MS);
  }

  /** Cắt timer và trả về null: template in nguyên văn đáp án, tức là "gõ xong" */
  private stopReveal(): void {
    if (this.revealTimer !== undefined) {
      clearInterval(this.revealTimer);
      this.revealTimer = undefined;
    }
    this.revealText.set(null);
  }

  /**
   * Chữ của một lượt: nếu hoạt cảnh đang chạy thì lượt CUỐI in theo số chữ đã hé
   * lộ, mọi lượt khác (và mọi lượt mở lại từ DB) in nguyên văn.
   */
  protected displayText(turn: ChatMessage, index: number): string {
    const revealed = this.revealText();
    if (revealed === null || !this.isRevealingTurn(index)) {
      return turn.content;
    }
    return revealed;
  }

  /**
   * Lượt có hoạt cảnh đang chạy là lượt cuối — lượt cũ in nguyên văn nên không
   * bị ẩn theo. Template dùng cờ này để chưa vẽ khối nguồn/biểu đồ của lượt
   * đang gõ dở: chip `[^n]` trỏ vào mục chưa hiện sẽ nhảy vô chỗ trống.
   */
  protected isRevealingTurn(index: number): boolean {
    return this.revealText() !== null && index === this.turns().length - 1;
  }

  /** Hệ thống yêu cầu giảm chuyển động thì in hết một lần, không gõ giả lập */
  private prefersReducedMotion(): boolean {
    return window.matchMedia?.('(prefers-reduced-motion: reduce)').matches === true;
  }

  private scrollToBottom(behavior: ScrollBehavior = 'smooth'): void {
    afterNextRender(
      () => {
        const element = this.scroller()?.nativeElement;
        element?.scrollTo({ top: element.scrollHeight, behavior });
      },
      { injector: this.injector },
    );
  }
}
