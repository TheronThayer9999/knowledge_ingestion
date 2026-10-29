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
  type LlmProviderKind,
  type LlmProviderOption,
  type LlmUserProviderForm,
} from '../../core/llm/llm.models';
import { LlmService } from '../../core/llm/llm.service';
import { MessageService } from '../../shared/feedback/message.service';

/**
 * Bản nháp form kết nối riêng. Mọi ô là string cho đơn giản khi bind
 * (selectionChange/input đều trả string), toForm() mới gọn thành DTO.
 */
interface MyProviderDraft {
  label: string;
  type: string;
  base_url: string;
  api_key: string;
  model: string;
}

/** Ô nhập của form — trùng tên field DTO để bản nháp gửi lên không phải đổi tên. */
type MyDraftField = 'label' | 'type' | 'base_url' | 'api_key' | 'model';

/**
 * Popup "Kết nối của tôi" (BYOK): mỗi tài khoản tự thêm provider LLM riêng —
 * base URL + API key của họ — rồi dùng cho lượt chat của chính họ.
 *
 * Khác popup của admin ở vài điểm cốt lõi:
 *   - KHÔNG có slug: backend tự sinh slug "u_<id>" nên user không phải nghĩ về
 *     tên khoá, cũng không tranh chỗ với slug của admin.
 *   - KHÔNG có nút tắt/bật: kết nối riêng chỉ có "dùng" và "xoá"; muốn ngưng thì
 *     chỉ cần không chọn nó ở menu chat.
 *   - API key đi MỘT CHIỀU: nhập vào → server mã hoá rồi lưu; không có lời gọi
 *     nào đọc key về, nên bản nháp chỉ giữ thứ user vừa gõ. Server chỉ trả cờ
 *     "đã có key".
 */
@Component({
  selector: 'app-my-llm-providers-dialog',
  imports: [
    MatDialogModule,
    MatButtonModule,
    MatIconModule,
    MatFormFieldModule,
    MatInputModule,
    MatSelectModule,
  ],
  templateUrl: './my-llm-providers-dialog.html',
  styleUrl: './my-llm-providers-dialog.scss',
})
export class MyLlmProvidersDialog {
  protected readonly lang = inject(LangService);
  private readonly llm = inject(LlmService);
  private readonly messages = inject(MessageService);

  /**
   * Hai loại provider backend dựng được. "openai-compatible" là cách gọi quen
   * thuộc của Groq/OpenRouter/NVIDIA — server chuẩn hoá về khuôn "openai".
   */
  protected readonly kinds: readonly LlmProviderKind[] = ['openai-compatible', 'ollama'];

  protected readonly providers = signal<LlmProviderOption[]>([]);
  protected readonly loading = signal(true);
  protected readonly failed = signal(false);
  /** id của hàng đang mở; null = thu gọn hết */
  protected readonly expanded = signal<number | null>(null);
  /** id đang ghi — khoá mọi nút ghi để tránh bấm đúp */
  protected readonly busy = signal<number | null>(null);
  /** id đang tải danh sách model */
  protected readonly modelsBusy = signal<number | null>(null);
  protected readonly error = signal('');
  /** form "Thêm kết nối" — ẩn cho tới khi user bấm nút */
  protected readonly showCreate = signal(false);
  protected readonly creating = signal(false);
  protected readonly create = signal<MyProviderDraft>(defaultCreateDraft());
  /** Bản nháp sửa theo id — chỉ chứa cái user vừa gõ */
  private readonly drafts = signal<Record<number, MyProviderDraft>>({});
  /** Model user chọn trong select nhưng chưa lưu */
  private readonly modelDrafts = signal<Record<number, string>>({});

  constructor() {
    this.load();
  }

  protected toggle(item: LlmProviderOption): void {
    const id = item.id ?? 0;
    this.expanded.update((open) => (open === id ? null : id));
  }

  protected kindLabel(kind: string): TranslationKey {
    return kind === 'ollama' ? 'SETTINGS_LLM_KIND_OLLAMA' : 'SETTINGS_LLM_KIND_OPENAI';
  }

  // ---- form tạo mới ----

  protected toggleCreate(): void {
    this.showCreate.update((open) => !open);
    this.create.set(defaultCreateDraft());
    this.error.set('');
  }

  protected onCreateInput(field: MyDraftField, event: Event): void {
    this.create.update((draft) => ({ ...draft, [field]: valueOf(event) }));
  }

  protected setCreateType(kind: string): void {
    this.create.update((draft) => ({ ...draft, type: kind }));
  }

  /** Tạo mới cần đủ nhãn + giao thức + base URL — không có gì để "giữ". */
  protected get canCreate(): boolean {
    const draft = this.create();
    return (
      draft.label.trim() !== '' &&
      draft.type !== '' &&
      draft.base_url.trim() !== '' &&
      !this.creating()
    );
  }

  protected createProvider(): void {
    if (!this.canCreate) {
      return;
    }
    this.creating.set(true);
    this.error.set('');
    this.llm.saveMyProvider(0, toForm(this.create())).subscribe({
      next: (option) => {
        this.creating.set(false);
        this.showCreate.set(false);
        this.create.set(defaultCreateDraft());
        this.providers.update((list) => [...list, option]);
        this.expanded.set(option.id ?? null);
        this.messages.success('SETTINGS_LLM_MY_CREATED');
      },
      error: (err: unknown) => {
        this.creating.set(false);
        this.fail(err);
      },
    });
  }

  // ---- form sửa ----

  /**
   * Giá trị ô định nghĩa: bản nháp nếu có, không thì đúng giá trị đang chạy.
   * Server bắt buộc có nhãn + giao thức + base URL nên mỗi lần lưu đều gửi
   * đủ ba — không gửi thì backend trả 400.
   */
  protected defValue(item: LlmProviderOption, field: MyDraftField): string {
    const id = item.id ?? 0;
    const draft = this.drafts()[id];
    const mine = draft ? draft[field] : undefined;
    if (mine !== undefined && mine !== '') {
      return mine;
    }
    if (field === 'label') {
      return item.label;
    }
    if (field === 'type') {
      return toProviderKind(item.type);
    }
    if (field === 'base_url') {
      return item.baseUrl ?? '';
    }
    if (field === 'model') {
      return item.defaultModel;
    }
    return '';
  }

  protected onDefInput(item: LlmProviderOption, field: MyDraftField, event: Event): void {
    const id = item.id ?? 0;
    this.drafts.update((all) => ({
      ...all,
      [id]: { ...emptyDraft(), ...all[id], [field]: valueOf(event) },
    }));
  }

  protected setDefType(item: LlmProviderOption, kind: string): void {
    const id = item.id ?? 0;
    this.drafts.update((all) => ({
      ...all,
      [id]: { ...emptyDraft(), ...all[id], type: kind },
    }));
  }

  /** Nút "Lưu" chỉ bật khi user thực sự sửa gì đó của hàng này. */
  protected hasDefChanges(item: LlmProviderOption): boolean {
    return this.drafts()[item.id ?? 0] !== undefined;
  }

  protected saveDef(item: LlmProviderOption): void {
    const id = item.id ?? 0;
    const draft = this.drafts()[id];
    if (!draft || this.busy() || this.modelsBusy()) {
      return;
    }
    this.busy.set(id);
    this.error.set('');
    this.llm.saveMyProvider(id, this.formOf(item, {})).subscribe({
      next: (option) => {
        this.busy.set(null);
        this.replace(option);
        this.clearDraft(id);
        this.messages.success('SETTINGS_LLM_MY_SAVED');
      },
      error: (err: unknown) => {
        this.busy.set(null);
        this.fail(err);
      },
    });
  }

  protected removeProvider(item: LlmProviderOption): void {
    const id = item.id ?? 0;
    if (this.busy()) {
      return;
    }
    this.busy.set(id);
    this.error.set('');
    this.llm.deleteMyProvider(id).subscribe({
      next: (list) => {
        this.busy.set(null);
        this.providers.set(list);
        this.expanded.update((open) => (open === id ? null : open));
        this.messages.success('SETTINGS_LLM_MY_DELETED');
      },
      error: (err: unknown) => {
        this.busy.set(null);
        this.fail(err);
      },
    });
  }

  // ---- model ----

  /** Model đang chọn trong select: bản nháp, chưa có thì model mặc định */
  protected modelValue(item: LlmProviderOption): string {
    return this.modelDrafts()[item.id ?? 0] ?? item.defaultModel;
  }

  protected setModelDraft(item: LlmProviderOption, model: string): void {
    this.modelDrafts.update((drafts) => ({ ...drafts, [item.id ?? 0]: model }));
  }

  /** Gọi chính provider bằng key của user để lấy danh sách model */
  protected loadModels(item: LlmProviderOption): void {
    const id = item.id ?? 0;
    if (this.modelsBusy() || this.busy()) {
      return;
    }
    this.modelsBusy.set(id);
    this.error.set('');
    this.llm.loadMyProviderModels(id).subscribe({
      next: (models) => {
        this.modelsBusy.set(null);
        this.patch(id, { models });
        this.messages.success('SETTINGS_LLM_MODEL_LOADED');
      },
      error: (err: unknown) => {
        this.modelsBusy.set(null);
        this.fail(err);
      },
    });
  }

  /** Chọn model mặc định cho kết nối này */
  protected useModel(item: LlmProviderOption): void {
    const id = item.id ?? 0;
    const model = this.modelValue(item);
    if (!model || this.busy() || this.modelsBusy()) {
      return;
    }
    this.busy.set(id);
    this.error.set('');
    this.llm.saveMyProvider(id, this.formOf(item, { model })).subscribe({
      next: (option) => {
        this.busy.set(null);
        this.replace(option);
        this.modelDrafts.update((drafts) => ({ ...drafts, [id]: '' }));
        this.messages.success('SETTINGS_LLM_MODEL_SAVED');
      },
      error: (err: unknown) => {
        this.busy.set(null);
        this.fail(err);
      },
    });
  }

  /** Server chỉ trả cờ "đã có key" — không bao giờ trả giá trị key. */
  protected hasKey(item: LlmProviderOption): boolean {
    return item.hasKey === true;
  }

  // ---- nội bộ ----

  /**
   * Form đầy đủ của một hàng: giá trị đang chạy, đè bản nháp user gõ (ô để
   * trống thì giữ giá trị cũ), rồi đè `change` (vd model vừa chọn).
   *
   * Luôn gửi đủ nhãn + giao thức + base URL vì backend bắt buộc có ba cái đó —
   * bản nháp chỉ chứa ô user vừa gõ nên không thể gửi trực tiếp.
   */
  private formOf(item: LlmProviderOption, change: Partial<MyProviderDraft>): LlmUserProviderForm {
    const draft = this.drafts()[item.id ?? 0] ?? emptyDraft();
    const merged: MyProviderDraft = {
      label: draft.label || item.label,
      type: draft.type || toProviderKind(item.type),
      base_url: draft.base_url || (item.baseUrl ?? ''),
      api_key: draft.api_key,
      model: draft.model || item.defaultModel,
      ...change,
    };
    return toForm(merged);
  }

  private replace(option: LlmProviderOption): void {
    this.providers.update((list) =>
      list.map((item) => (item.id === option.id ? option : item)),
    );
  }

  private patch(id: number, change: Partial<LlmProviderOption>): void {
    this.providers.update((list) =>
      list.map((item) => (item.id === id ? { ...item, ...change } : item)),
    );
  }

  private clearDraft(id: number): void {
    this.drafts.update((all) => {
      const rest = { ...all };
      delete rest[id];
      return rest;
    });
  }

  private load(): void {
    this.loading.set(true);
    this.failed.set(false);
    this.llm.listMyProviders().subscribe({
      next: (list) => {
        this.loading.set(false);
        this.providers.set(list);
        // Mở sẵn kết nối đầu tiên — user thấy ngay base URL và trạng thái
        // key thay vì phải bấm mở từng hàng.
        this.expanded.set(list[0]?.id ?? null);
      },
      error: () => {
        this.loading.set(false);
        this.failed.set(true);
      },
    });
  }

  /** Lỗi server đã là tiếng Việt và được phép lộ ra; lỗi khác dùng key chung */
  private fail(err: unknown): void {
    const message = (err as { message?: unknown } | null)?.message;
    this.error.set(
      typeof message === 'string' && message.trim() ? message : this.lang.t('SETTINGS_LLM_MY_FAILED'),
    );
  }
}

function emptyDraft(): MyProviderDraft {
  return { label: '', type: '', base_url: '', api_key: '', model: '' };
}

/** Bản nháp form "Thêm kết nối" — giao thức mặc định là openai-compatible. */
function defaultCreateDraft(): MyProviderDraft {
  return { ...emptyDraft(), type: 'openai-compatible' };
}

/**
 * Chỉ gửi field có giá trị: key rỗng = giữ key đang có, model rỗng = giữ model
 * đang có, nên sửa một ô không vô tình xoá danh sách model đã tải.
 */
function toForm(draft: MyProviderDraft): LlmUserProviderForm {
  const form: LlmUserProviderForm = {
    label: draft.label.trim(),
    type: draft.type === 'ollama' ? 'ollama' : 'openai-compatible',
    base_url: draft.base_url.trim(),
  };
  const apiKey = draft.api_key.trim();
  if (apiKey) {
    form.api_key = apiKey;
  }
  const model = draft.model.trim();
  if (model) {
    form.model = model;
  }
  return form;
}

function valueOf(event: Event): string {
  return (event.target as HTMLInputElement).value;
}
