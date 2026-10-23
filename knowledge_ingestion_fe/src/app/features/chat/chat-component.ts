import { Component, inject, SecurityContext, signal } from '@angular/core';
import { DomSanitizer } from '@angular/platform-browser';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatIconModule } from '@angular/material/icon';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { MatProgressSpinnerModule } from '@angular/material/progress-spinner';
import { RouterLink } from '@angular/router';
import { marked } from 'marked';
import { CategoriesService } from '../../core/api/categories.service';
import { ChatService } from '../../core/api/chat.service';
import { ApiRequestError } from '../../core/api/api.helpers';
import { ChatCite, ChatHistoryItem, ChatRequest, CategoryNode } from '../../core/api/api.models';
import { MessageService } from '../../shared/feedback/message.service';
import { LangService } from '../../shared/i18n/lang.service';

/** Một lượt hiển thị trong khung chat (history gửi lên BE lấy từ đây). */
interface ChatTurn {
  role: 'user' | 'assistant';
  content: string;
  /** Answer của assistant render markdown -> HTML đã sanitize (bind innerHTML). */
  html?: string;
  cites?: ChatCite[];
  questionType?: string;
}

// BE binding: history tối đa ~10 lượt, mỗi content <= 4000 ký tự.
const HISTORY_MAX_TURNS = 10;
const HISTORY_MAX_CHARS = 4000;

/**
 * Trang hỏi đáp RAG: gửi câu hỏi + history các vòng trước, BE trả answer
 * (Markdown, có footnote [^n]) + cites. Lịch sử chỉ giữ in-memory (không có
 * endpoint lưu hội thoại bên BE).
 */
@Component({
  selector: 'app-chat',
  imports: [
    ReactiveFormsModule,
    MatButtonModule,
    MatCardModule,
    MatFormFieldModule,
    MatIconModule,
    MatInputModule,
    MatProgressSpinnerModule,
    MatSelectModule,
    RouterLink,
  ],
  templateUrl: './chat-component.html',
  styleUrl: './chat-component.scss',
})
export class ChatComponent {
  private readonly chatApi = inject(ChatService);
  private readonly categoriesApi = inject(CategoriesService);
  private readonly messages = inject(MessageService);
  private readonly sanitizer = inject(DomSanitizer);
  private readonly fb = inject(FormBuilder);
  protected readonly lang = inject(LangService);

  protected readonly turns = signal<ChatTurn[]>([]);
  protected readonly sending = signal(false);
  protected readonly categoryOptions = signal<{ id: number; label: string }[]>([]);

  protected readonly form = this.fb.nonNullable.group({
    input: ['', [Validators.required, Validators.minLength(2)]],
    provider: [''],
    model: [''],
    categoryIds: [[] as number[]],
  });

  constructor() {
    this.categoriesApi.list().subscribe({
      next: (res) => {
        this.categoryOptions.set(this.orderTree(res.categories).map((n) => ({ id: n.id, label: `${'\u00A0\u00A0'.repeat(n.depth)}${n.name}` })));
      },
      error: (err: unknown) => this.showError(err),
    });
  }

  protected send(): void {
    if (this.form.invalid || this.sending()) {
      return;
    }
    const v = this.form.getRawValue();
    const text = v.input.trim();
    const history = this.buildHistory();

    this.turns.update((list) => [...list, { role: 'user', content: text }]);
    this.form.patchValue({ input: '' });
    this.sending.set(true);

    const req: ChatRequest = { message: text, history };
    if (v.provider.trim()) {
      req.provider = v.provider.trim();
    }
    if (v.model.trim()) {
      req.model = v.model.trim();
    }
    if (v.categoryIds.length > 0) {
      req.category_ids = v.categoryIds;
    }
    this.chatApi.chat(req).subscribe({
      next: (res) => {
        this.turns.update((list) => [
          ...list,
          {
            role: 'assistant',
            content: res.answer,
            html: this.renderMarkdown(res.answer),
            cites: res.cites,
            questionType: res.question_type,
          },
        ]);
        this.sending.set(false);
      },
      error: (err: unknown) => {
        this.sending.set(false);
        this.showError(err);
      },
    });
  }

  protected clear(): void {
    this.turns.set([]);
  }

  /** Vẽ markdown qua `marked`, tự sanitize HTML — binding innerHTML cũng tự sanitize lần nữa. */
  private renderMarkdown(md: string): string {
    const html = marked.parse(md, { async: false }) as string;
    return this.sanitizer.sanitize(SecurityContext.HTML, html) ?? '';
  }

  /** History từ các lượt trả lời thành công gần nhất, cắt theo binding BE. */
  private buildHistory(): ChatHistoryItem[] {
    return this.turns()
      .slice(-HISTORY_MAX_TURNS)
      .map((t) => ({ role: t.role, content: t.content.slice(0, HISTORY_MAX_CHARS) }));
  }

  private orderTree(list: CategoryNode[]): CategoryNode[] {
    const byParent = new Map<number | null, CategoryNode[]>();
    for (const n of list) {
      const key = n.parent_id ?? null;
      const bucket = byParent.get(key) ?? [];
      bucket.push(n);
      byParent.set(key, bucket);
    }
    const out: CategoryNode[] = [];
    const walk = (parent: number | null): void => {
      for (const n of byParent.get(parent) ?? []) {
        out.push(n);
        walk(n.id);
      }
    };
    walk(null);
    return out;
  }

  private showError(err: unknown): void {
    if (err instanceof ApiRequestError) {
      this.messages.raw('error', err.message);
    } else {
      this.messages.error('LOGIN_ERRORS_GENERIC');
    }
  }
}
