import { TextFieldModule } from '@angular/cdk/text-field';
import {
  Component,
  DestroyRef,
  ElementRef,
  Injector,
  afterNextRender,
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
import type { Subscription } from 'rxjs';
import type { ChatCite, ChatTurn } from '../../core/chat/chat.models';
import { ChatService } from '../../core/chat/chat.service';
import { LangService, type TranslationKey } from '../../core/i18n/lang.service';
import type { Category } from '../../core/knowledge/knowledge.models';
import { KnowledgeService } from '../../core/knowledge/knowledge.service';
import { MessageService } from '../../shared/feedback/message.service';
import { MarkdownPipe } from '../../shared/markdown/markdown.pipe';
import {
  DocumentDetailDialog,
  type DocumentDetailData,
} from '../documents/dialogs/document-detail-dialog';

interface ChatMessage extends ChatTurn {
  cites?: ChatCite[];
  /** Phần model suy nghĩ (chỉ có ở lượt bật suy luận sâu) — hiển thị dạng gập */
  reasoning?: string;
}

const SUGGESTIONS: TranslationKey[] = ['CHAT_SUGGEST_1', 'CHAT_SUGGEST_2', 'CHAT_SUGGEST_3'];

/** Giữ tối đa 12 lượt gần nhất khi gửi history — đủ ngữ cảnh, không phình payload */
const HISTORY_LIMIT = 12;

/**
 * Chat RAG: trả lời một lần (không streaming) kèm trích dẫn.
 * Phạm vi (category) khoanh vùng tài liệu cho agent tìm kiếm.
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
  ],
  templateUrl: './chat.html',
  styleUrl: './chat.scss',
})
export class Chat {
  protected readonly lang = inject(LangService);
  private readonly chat = inject(ChatService);
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
  /** Sidebar tiện ích (phạm vi + phiên mới) — ẩn đi để khung chat rộng hơn */
  protected readonly panelOpen = signal(true);
  protected readonly suggestions = SUGGESTIONS;

  /** Request đang bay — giữ subscription để nút Dừng hủy được giữa chừng */
  private pending?: Subscription;

  private readonly scroller = viewChild<ElementRef<HTMLElement>>('scroller');

  constructor() {
    // Danh mục chỉ phục vụ chọn phạm vi — lỗi thì vẫn chat toàn kho được
    this.knowledge.listCategories().subscribe({
      next: (categories) => this.categories.set(categories),
      error: () => this.categories.set([]),
    });
    // Rời trang giữa lúc đang chờ câu trả lời thì đừng để request treo
    this.destroyRef.onDestroy(() => this.pending?.unsubscribe());
  }

  protected toggleThinking(): void {
    this.thinking.update((value) => !value);
  }

  /**
   * Nhóm tin cùng người gửi: tin liền trước cùng role thì siết khoảng cách
   * lại (kiểu iMessage/Gmail) — đọc thấy rõ cụm của một người.
   */
  protected isGrouped(index: number): boolean {
    const turns = this.turns();
    return index > 0 && turns[index - 1]?.role === turns[index]?.role;
  }

  protected togglePanel(): void {
    this.panelOpen.update((value) => !value);
  }

  protected onInput(event: Event): void {
    this.draft.set((event.target as HTMLTextAreaElement).value);
  }

  protected onScopeChange(value: number): void {
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

  protected reset(): void {
    this.turns.set([]);
    this.draft.set('');
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
   * Dừng lúc model đang trả lời: hủy request, bỏ lượt user vừa ghi vào
   * transcript và trả câu hỏi về ô soạn — chưa có đáp án nên giữ nó trong
   * history chỉ tổ làm nhiễu ngữ cảnh lượt sau.
   */
  protected stop(): void {
    this.pending?.unsubscribe();
    this.pending = undefined;
    this.sending.set(false);
    const turns = this.turns();
    const last = turns[turns.length - 1];
    if (last?.role === 'user') {
      this.turns.set(turns.slice(0, -1));
      this.draft.set(last.content);
    }
  }

  private ask(text: string): void {
    const question = text.trim();
    if (question === '' || this.sending()) {
      return;
    }
    const history = this.turns()
      .slice(-HISTORY_LIMIT)
      .map(({ role, content }) => ({ role, content }));
    this.turns.update((list) => [...list, { role: 'user', content: question }]);
    this.draft.set('');
    this.sending.set(true);
    this.scrollToBottom();
    this.pending = this.chat
      .send(question, history, this.scope() || null, this.thinking())
      .subscribe({
        next: (answer) => {
          this.pending = undefined;
          this.turns.update((list) => [
            ...list,
            {
              role: 'assistant',
              content: answer.answer,
              cites: answer.cites,
              reasoning: answer.reasoning,
            },
          ]);
          this.sending.set(false);
          this.scrollToBottom();
        },
        error: (error: unknown) => {
          this.pending = undefined;
          this.sending.set(false);
          this.messages.errorFromApi(error, 'AUTH_ERROR_GENERIC');
          this.scrollToBottom();
        },
      });
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
