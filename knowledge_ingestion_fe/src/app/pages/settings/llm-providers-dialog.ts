import { Component, inject, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatDialogModule } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { LangService, type TranslationKey } from '../../core/i18n/lang.service';
import {
  toProviderKind,
  type LlmProviderForm,
  type LlmProviderKind,
  type LlmProviderOption,
} from '../../core/llm/llm.models';
import { LlmService } from '../../core/llm/llm.service';
import { MessageService } from '../../shared/feedback/message.service';

/**
 * Bản nháp một form provider. Để mọi ô là string cho đơn giản khi bind
 * (selectionChange/input đều trả string), toForm() mới gọn thành DTO.
 */
interface ProviderDraft {
  slug: string;
  label: string;
  type: string;
  base_url: string;
  api_key: string;
  disabled?: boolean;
}

/** Ô nhập của form — trùng tên field DTO để bản nháp gửi lên không phải đổi tên. */
type DraftField = 'slug' | 'label' | 'type' | 'base_url' | 'api_key';

/**
 * Popup quản trị kết nối AI (Đợt 28 nấc 2 + 3). Trước đây provider chỉ là thứ
 * khai trong `configs/config.json` và popup thêm được mỗi key/model; từ nấc 3
 * admin tạo/sửa/tắt/xoá cả ĐỊNH NGHĨA provider (loại, base_url, nhãn) ngay ở
 * đây — phần khai trong file vẫn là mặc định, DB thắng khi có giá trị, và mọi
 * thay đổi có hiệu lực ngay, không cần restart app.
 *
 * Key vẫn đi MỘT CHIỀU: nhập vào, server mã hoá rồi lưu DB; không có lời gọi
 * nào đọc key về trình duyệt, nên bản nháp chỉ giữ thứ admin vừa gõ.
 *
 * Mỗi hàng là một provider, mở ra mới hiện form (giữ popup gọn khi nhiều
 * provider). Chỉ một hàng mở tại một thời điểm.
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

  /**
   * Hai loại provider backend dựng được. "openai-compatible" là cách gọi quen
   * thuộc của Groq/OpenRouter/9Router/NVIDIA — server chuẩn hoá về khuôn "openai".
   */
  protected readonly kinds: readonly LlmProviderKind[] = ['openai-compatible', 'ollama'];

  protected readonly providers = signal<LlmProviderOption[]>([]);
  protected readonly loading = signal(true);
  protected readonly failed = signal(false);
  /** slug của hàng đang mở; null = thu gọn hết */
  protected readonly expanded = signal<string | null>(null);
  /** slug đang ghi key/model — khoá mọi nút ghi để tránh bấm đúp */
  protected readonly busy = signal<string | null>(null);
  /** slug đang ghi ĐỊNH NGHĨA (lưu form / tắt / bật / xoá) */
  protected readonly savingDef = signal<string | null>(null);
  protected readonly modelsBusy = signal<string | null>(null);
  protected readonly error = signal('');
  /** form "Thêm provider" — ẩn cho tới khi admin bấm nút */
  protected readonly showCreate = signal(false);
  protected readonly creating = signal(false);
  protected readonly create = signal<ProviderDraft>(defaultCreateDraft());
  /** Bản nháp định nghĩa theo slug — chỉ chứa cái admin sửa */
  private readonly drafts = signal<Record<string, ProviderDraft>>({});
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

  protected kindLabel(kind: string): TranslationKey {
    return kind === 'ollama' ? 'SETTINGS_LLM_KIND_OLLAMA' : 'SETTINGS_LLM_KIND_OPENAI';
  }

  // ---- form định nghĩa provider ----

  protected toggleCreate(): void {
    this.showCreate.update((open) => !open);
    this.create.set(defaultCreateDraft());
    this.error.set('');
  }

  protected onCreateInput(field: DraftField, event: Event): void {
    this.create.update((draft) => ({ ...draft, [field]: valueOf(event) }));
  }

  protected setCreateType(kind: string): void {
    this.create.update((draft) => ({ ...draft, type: kind }));
  }

  /** Slug là khoá DB và đi vào URL path, nên phải đúng khuôn ngay trên form. */
  protected get canCreate(): boolean {
    const draft = this.create();
    return (
      /^[a-z0-9][a-z0-9._-]{0,63}$/.test(draft.slug.trim().toLowerCase()) &&
      draft.type !== '' &&
      !this.creating()
    );
  }

  protected createProvider(): void {
    if (!this.canCreate) {
      return;
    }
    const draft = this.create();
    const slug = draft.slug.trim().toLowerCase();
    this.creating.set(true);
    this.error.set('');
    this.llm.saveProvider(slug, toForm(draft)).subscribe({
      next: (options) => {
        this.creating.set(false);
        this.showCreate.set(false);
        this.create.set(defaultCreateDraft());
        this.applyList(options.providers, slug);
        this.messages.success('SETTINGS_LLM_PROVIDER_CREATED');
      },
      error: (err: unknown) => {
        this.creating.set(false);
        this.fail(err);
      },
    });
  }

  /** Giá trị ô định nghĩa: bản nháp nếu có, không thì đúng giá trị đang chạy. */
  protected defValue(item: LlmProviderOption, field: Exclude<DraftField, 'slug' | 'api_key'>) {
    const draft = this.drafts()[item.slug];
    const mine = draft ? draft[field] : undefined;
    if (mine !== undefined && mine !== '') {
      return mine;
    }
    if (field === 'label') {
      return item.label;
    }
    return field === 'type' ? toProviderKind(item.type) : (item.baseUrl ?? '');
  }

  protected onDefInput(slug: string, field: DraftField, event: Event): void {
    this.drafts.update((all) => ({
      ...all,
      [slug]: { ...emptyDraft(), ...all[slug], [field]: valueOf(event) },
    }));
  }

  protected setDefType(slug: string, kind: string): void {
    this.drafts.update((all) => ({
      ...all,
      [slug]: { ...emptyDraft(), ...all[slug], type: kind },
    }));
  }

  /** Nút "Lưu định nghĩa" chỉ bật khi admin thực sự sửa gì đó của hàng này. */
  protected hasDefChanges(item: LlmProviderOption): boolean {
    return this.drafts()[item.slug] !== undefined;
  }

  protected saveDef(item: LlmProviderOption): void {
    const draft = this.drafts()[item.slug];
    if (!draft || this.busy() || this.savingDef()) {
      return;
    }
    this.patchDef(item.slug, toForm(draft));
  }

  /** Tắt/bật provider: giữ nguyên key, model, danh sách model — chỉ cái công tắc. */
  protected toggleDisabled(item: LlmProviderOption): void {
    if (this.busy() || this.savingDef()) {
      return;
    }
    this.patchDef(item.slug, { disabled: item.disabled !== true });
  }

  protected removeProvider(item: LlmProviderOption): void {
    if (this.busy() || this.savingDef()) {
      return;
    }
    this.savingDef.set(item.slug);
    this.error.set('');
    this.llm.deleteProvider(item.slug).subscribe({
      next: (options) => {
        this.savingDef.set(null);
        this.applyList(options.providers, null);
        this.messages.success('SETTINGS_LLM_PROVIDER_DELETED');
      },
      error: (err: unknown) => {
        this.savingDef.set(null);
        this.fail(err);
      },
    });
  }

  /** Nhãn nguồn provider — admin cần biết "xoá là quay về file" hay "xoá là mất hẳn". */
  protected sourceKey(item: LlmProviderOption): TranslationKey {
    return item.fromConfig ? 'SETTINGS_LLM_SOURCE_CONFIG' : 'SETTINGS_LLM_SOURCE_DB';
  }

  protected deleteHintKey(item: LlmProviderOption): TranslationKey {
    return item.fromConfig ? 'SETTINGS_LLM_DELETE_HINT_CONFIG' : 'SETTINGS_LLM_DELETE_HINT_DB';
  }

  // ---- key + model (nấc 2, giữ luồng riêng) ----

  protected keyDraft(slug: string): string {
    return this.keyDrafts()[slug] ?? '';
  }

  protected onKeyInput(slug: string, event: Event): void {
    this.keyDrafts.update((drafts) => ({ ...drafts, [slug]: valueOf(event) }));
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

  // ---- nội bộ ----

  /** Ghi định nghĩa rồi nạp lại cả danh sách (backend trả list sau mỗi lần sửa). */
  private patchDef(slug: string, form: LlmProviderForm): void {
    this.savingDef.set(slug);
    this.error.set('');
    this.llm.saveProvider(slug, form).subscribe({
      next: (options) => {
        this.savingDef.set(null);
        this.clearDraft(slug);
        this.applyList(options.providers, this.expanded() === slug ? slug : null);
        this.messages.success('SETTINGS_LLM_PROVIDER_SAVED');
      },
      error: (err: unknown) => {
        this.savingDef.set(null);
        this.fail(err);
      },
    });
  }

  private applyList(list: LlmProviderOption[], openSlug: string | null): void {
    this.providers.set(list);
    if (openSlug) {
      this.expanded.set(openSlug);
    }
  }

  private clearDraft(slug: string): void {
    this.drafts.update((all) => {
      const rest = { ...all };
      delete rest[slug];
      return rest;
    });
  }

  private setKeyDraft(slug: string, value: string): void {
    this.keyDrafts.update((drafts) => ({ ...drafts, [slug]: value }));
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

function emptyDraft(): ProviderDraft {
  return { slug: '', label: '', type: '', base_url: '', api_key: '' };
}

/**
 * Bản nháp form "Thêm provider". Loại để TRỐNG chứ không mặc định
 * "openai-compatible": select phải hiện placeholder, còn form sửa định nghĩa của
 * một hàng thì bản nháp rỗng là cần — toForm() chỉ gửi field có giá trị, nên
 * kiểu thừa vào bản nháp sẽ vô tình ghi đè type của provider ollama.
 */
function defaultCreateDraft(): ProviderDraft {
  return { ...emptyDraft(), type: 'openai-compatible' };
}

/**
 * Chỉ gửi field có giá trị: backend hiểu "không gửi = giữ nguyên", nên form
 * sửa mỗi nhãn không vô tình xoá base_url/key đã lưu.
 */
function toForm(draft: ProviderDraft): LlmProviderForm {
  const form: LlmProviderForm = {};
  const label = draft.label.trim();
  if (label) {
    form.label = label;
  }
  if (draft.type === 'ollama' || draft.type === 'openai-compatible') {
    form.type = draft.type;
  }
  const baseUrl = draft.base_url.trim();
  if (baseUrl) {
    form.base_url = baseUrl;
  }
  const apiKey = draft.api_key.trim();
  if (apiKey) {
    form.api_key = apiKey;
  }
  if (draft.disabled !== undefined) {
    form.disabled = draft.disabled;
  }
  return form;
}

function valueOf(event: Event): string {
  return (event.target as HTMLInputElement).value;
}
