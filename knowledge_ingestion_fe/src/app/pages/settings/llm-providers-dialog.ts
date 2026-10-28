import { Component, inject, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatDialogModule } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { LangService, type TranslationKey } from '../../core/i18n/lang.service';
import type { LlmProviderOption } from '../../core/llm/llm.models';
import { LlmService } from '../../core/llm/llm.service';
import { MessageService } from '../../shared/feedback/message.service';

/**
 * Popup quản trị kết nối AI (Đợt 28 nấc 2): provider vẫn KHAI BÁO trong
 * `configs/config.json` — popup chỉ thêm key, tải danh sách model và chọn model
 * mặc định. Key nhập ở đây được mã hoá ở server và lưu DB, đè key trong file;
 * xoá key là quay về dùng file. Không có lời gọi nào đọc key về trình duyệt.
 *
 * Mỗi hàng là một provider, mở ra mới hiện ô key + model (giữ popup gọn khi
 * nhiều provider). Chỉ một hàng mở tại một thời điểm.
 */
@Component({
  selector: 'app-llm-providers-dialog',
  imports: [
    MatDialogModule,
    MatButtonModule,
    MatIconModule,
    MatFormFieldModule,
    MatInputModule,
    MatSelectModule,
  ],
  templateUrl: './llm-providers-dialog.html',
  styleUrl: './llm-providers-dialog.scss',
})
export class LlmProvidersDialog {
  protected readonly lang = inject(LangService);
  private readonly llm = inject(LlmService);
  private readonly messages = inject(MessageService);

  protected readonly providers = signal<LlmProviderOption[]>([]);
  protected readonly loading = signal(true);
  protected readonly failed = signal(false);
  /** slug của hàng đang mở; null = thu gọn hết */
  protected readonly expanded = signal<string | null>(null);
  /** slug đang ghi key/model — khoá mọi nút ghi để tránh bấm đúp */
  protected readonly busy = signal<string | null>(null);
  protected readonly modelsBusy = signal<string | null>(null);
  protected readonly error = signal('');
  /** Bản nháp key theo slug — không bao giờ nhận key từ server, chỉ giữ cái admin gõ */
  private readonly keyDrafts = signal<Record<string, string>>({});
  /** Model admin chọn trong select nhưng chưa lưu */
  private readonly modelDrafts = signal<Record<string, string>>({});

  constructor() {
    this.load();
  }

  protected toggle(item: LlmProviderOption): void {
    this.expanded.update((slug) => (slug === item.slug ? null : item.slug));
  }

  protected keyDraft(slug: string): string {
    return this.keyDrafts()[slug] ?? '';
  }

  protected onKeyInput(slug: string, event: Event): void {
    const value = (event.target as HTMLInputElement).value;
    this.setKeyDraft(slug, value);
  }

  /** Model đang chọn trong select: bản nháp, chưa có thì model mặc định */
  protected modelValue(item: LlmProviderOption): string {
    return this.modelDrafts()[item.slug] ?? item.defaultModel;
  }

  protected setModelDraft(slug: string, model: string): void {
    this.modelDrafts.update((drafts) => ({ ...drafts, [slug]: model }));
  }

  /** Nhãn nguồn key hiện tại (không phải giá trị key) */
  protected keyStateKey(item: LlmProviderOption): TranslationKey {
    if (item.keySource === 'database') {
      return 'SETTINGS_LLM_KEY_FROM_DB';
    }
    if (item.keySource === 'config') {
      return 'SETTINGS_LLM_KEY_FROM_CONFIG';
    }
    return 'SETTINGS_LLM_KEY_NONE';
  }

  protected keyPlaceholderKey(item: LlmProviderOption): TranslationKey {
    if (item.keySource === 'database') {
      return 'SETTINGS_LLM_KEY_PLACEHOLDER_DB';
    }
    if (item.keySource === 'config') {
      return 'SETTINGS_LLM_KEY_PLACEHOLDER_CONFIG';
    }
    return 'SETTINGS_LLM_KEY_PLACEHOLDER_NONE';
  }

  protected saveKey(item: LlmProviderOption): void {
    const apiKey = this.keyDraft(item.slug).trim();
    if (!apiKey || this.busy()) {
      return;
    }
    this.busy.set(item.slug);
    this.error.set('');
    this.llm.saveKey(item.slug, apiKey).subscribe({
      next: (option) => {
        this.busy.set(null);
        this.replace(option);
        this.setKeyDraft(item.slug, '');
        this.messages.success('SETTINGS_LLM_KEY_SAVED');
      },
      error: (err: unknown) => {
        this.busy.set(null);
        this.fail(err);
      },
    });
  }

  protected clearKey(item: LlmProviderOption): void {
    if (this.busy()) {
      return;
    }
    this.busy.set(item.slug);
    this.error.set('');
    this.llm.clearKey(item.slug).subscribe({
      next: (option) => {
        this.busy.set(null);
        this.replace(option);
        this.setKeyDraft(item.slug, '');
        this.messages.success('SETTINGS_LLM_KEY_CLEARED');
      },
      error: (err: unknown) => {
        this.busy.set(null);
        this.fail(err);
      },
    });
  }

  /** Gọi chính provider để lấy danh sách model — backend đã lưu vào DB */
  protected loadModels(item: LlmProviderOption): void {
    if (this.modelsBusy() || this.busy()) {
      return;
    }
    this.modelsBusy.set(item.slug);
    this.error.set('');
    this.llm.loadModels(item.slug).subscribe({
      next: (models) => {
        this.modelsBusy.set(null);
        this.patch(item.slug, { models });
        this.messages.success('SETTINGS_LLM_MODEL_LOADED');
      },
      error: (err: unknown) => {
        this.modelsBusy.set(null);
        this.fail(err);
      },
    });
  }

  protected useModel(item: LlmProviderOption): void {
    const model = this.modelValue(item);
    if (!model || this.busy() || this.modelsBusy()) {
      return;
    }
    this.busy.set(item.slug);
    this.error.set('');
    this.llm.selectModel(item.slug, model).subscribe({
      next: (option) => {
        this.busy.set(null);
        this.replace(option);
        this.modelDrafts.update((drafts) => ({ ...drafts, [item.slug]: '' }));
        this.messages.success('SETTINGS_LLM_MODEL_SAVED');
      },
      error: (err: unknown) => {
        this.busy.set(null);
        this.fail(err);
      },
    });
  }

  private load(): void {
    this.loading.set(true);
    this.failed.set(false);
    this.llm.options().subscribe({
      next: (options) => {
        this.loading.set(false);
        this.providers.set(options.providers);
        // Mở sẵn provider mặc định để admin thấy ngay ô key/model
        const open = options.providers.find((item) => item.isDefault) ?? options.providers[0];
        this.expanded.set(open?.slug ?? null);
      },
      error: () => {
        this.loading.set(false);
        this.failed.set(true);
      },
    });
  }

  private setKeyDraft(slug: string, value: string): void {
    this.keyDrafts.update((drafts) => ({ ...drafts, [slug]: value }));
  }

  private replace(option: LlmProviderOption): void {
    this.providers.update((list) =>
      list.map((item) => (item.slug === option.slug ? option : item)),
    );
  }

  private patch(slug: string, change: Partial<LlmProviderOption>): void {
    this.providers.update((list) =>
      list.map((item) => (item.slug === slug ? { ...item, ...change } : item)),
    );
  }

  /** Lỗi server đã là tiếng Việt và được phép lộ ra; lỗi khác dùng key chung */
  private fail(err: unknown): void {
    const message = (err as { message?: unknown } | null)?.message;
    this.error.set(
      typeof message === 'string' && message.trim() ? message : this.lang.t('SETTINGS_LLM_FAILED'),
    );
  }
}
