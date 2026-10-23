import { TextFieldModule } from '@angular/cdk/text-field';
import {
  Component,
  ElementRef,
  Injector,
  afterNextRender,
  inject,
  signal,
  viewChild,
} from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatSelectModule } from '@angular/material/select';
import type { ChatCite, ChatTurn } from '../../core/chat/chat.models';
import { ChatService } from '../../core/chat/chat.service';
import { LangService, type TranslationKey } from '../../core/i18n/lang.service';
import type { Category } from '../../core/knowledge/knowledge.models';
import { KnowledgeService } from '../../core/knowledge/knowledge.service';
import { MessageService } from '../../shared/feedback/message.service';
import { MarkdownPipe } from '../../shared/markdown/markdown.pipe';

interface ChatMessage extends ChatTurn {
  cites?: ChatCite[];
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
  private readonly injector = inject(Injector);

  protected readonly categories = signal<Category[]>([]);
  protected readonly turns = signal<ChatMessage[]>([]);
  protected readonly draft = signal('');
  protected readonly scope = signal(0);
  protected readonly sending = signal(false);
  protected readonly suggestions = SUGGESTIONS;

  private readonly scroller = viewChild<ElementRef<HTMLElement>>('scroller');

  constructor() {
    // Danh mục chỉ phục vụ chọn phạm vi — lỗi thì vẫn chat toàn kho được
    this.knowledge.listCategories().subscribe({
      next: (categories) => this.categories.set(categories),
      error: () => this.categories.set([]),
    });
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
    this.chat.send(question, history, this.scope() || null).subscribe({
      next: (answer) => {
        this.turns.update((list) => [
          ...list,
          { role: 'assistant', content: answer.answer, cites: answer.cites },
        ]);
        this.sending.set(false);
        this.scrollToBottom();
      },
      error: (error: unknown) => {
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
