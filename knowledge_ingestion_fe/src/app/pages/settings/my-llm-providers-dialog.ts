import { Component, inject, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatDialogModule } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { LangService } from '../../core/i18n/lang.service';
import { compactForm, valueOf } from '../../core/llm/llm.form';
import {
  type LlmProviderCatalogItem,
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
  provider: string;
  api_key: string;
  model: string;
}

/** Ô nhập của form — trùng tên field DTO để bản nháp gửi lên không phải đổi tên. */
type MyDraftField = 'label' | 'provider' | 'api_key' | 'model';

/**
 * Popup "Kết nối của tôi" (BYOK): mỗi tài khoản gắn API key của riêng họ vào
 * một nhà cung cấp trong danh mục hệ thống, rồi dùng nó cho lượt chat của
 * chính họ.
 *
 * User KHÔNG tự gõ base URL: server tra định nghĩa của provider đã chọn
 * (giao thức + base URL) trong danh mục. Nhờ vậy không có đường nào để user
 * trỏ server sang host tuỳ ý, và đường dẫn luôn đúng sẵn — không phải đoán
 * mò xem provider thật nằm ở /v1 hay /api/v1.
 *
 * Khác popup của admin ở vài điểm cốt lõi:
 *   - KHÔNG có slug: backend tự sinh slug "u_<id>" nên user không phải nghĩ về
 *     tên khoá, cũng không tranh chỗ với slug của admin.
 *   - KHÔNG có ô base URL / giao thức: chọn provider là đủ, server tự tra.
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

  protected readonly providers = signal<LlmProviderOption[]>([]);
  /** Danh mục provider hệ thống — user chọn ở đây rồi dán key của họ */
  protected readonly catalog = signal<LlmProviderCatalogItem[]>([]);
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
    this.loadCatalog();
  }

  protected toggle(item: LlmProviderOption): void {
    const id = item.id ?? 0;
    this.expanded.update((open) => (open === id ? null : id));
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

  protected setCreateProvider(slug: string): void {
    this.create.update((draft) => ({ ...draft, provider: slug }));
  }

  /**
   * Tạo mới cần nhà cung cấp + API key — không có gì để "giữ". Provider ollama
   * (local) chạy không key nên ô key không bắt buộc với loại đó.
   */
  protected get canCreate(): boolean {
    const draft = this.create();
    const slug = draft.provider.trim();
    if (slug === '' || this.creating()) {
      return false;
    }
    return !this.needsKey(slug) || draft.api_key.trim() !== '';
  }

  /** Ollama local chạy không key; loại khác (openai-compatible) thì cần key. */
  private needsKey(slug: string): boolean {
    const entry = this.catalog().find((item) => item.slug === slug);
    return entry ? entry.type !== 'ollama' : true;
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
   * Sửa chỉ đổi nhãn / key / model — nhà cung cấp của hàng đã tạo không đổi.
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

  /** Tên nhà cung cấp của hàng đang mở — tra theo danh mục, không lộ base URL. */
  protected providerLabel(item: LlmProviderOption): string {
    const slug = item.provider ?? '';
    return this.catalog().find((entry) => entry.slug === slug)?.label ?? slug;
  }

  // ---- nội bộ ----

  /**
   * Form đầy đủ của một hàng: giá trị đang chạy, đè bản nháp user gõ (ô để
   * trống thì giữ giá trị cũ), rồi đè `change` (vd model vừa chọn).
   *
   * Chỉ gửi field có giá trị: key rỗng = giữ key đang có, model rỗng = giữ
   * model đang có — sửa một ô không vô tình xoá danh sách model đã tải.
   */
  private formOf(item: LlmProviderOption, change: Partial<MyProviderDraft>): LlmUserProviderForm {
    const draft = this.drafts()[item.id ?? 0] ?? emptyDraft();
    const merged: MyProviderDraft = {
      label: draft.label || item.label,
      provider: draft.provider || item.provider || '',
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
        // Mở sẵn kết nối đầu tiên — user thấy ngay trạng thái key thay vì
        // phải bấm mở từng hàng.
        this.expanded.set(list[0]?.id ?? null);
      },
      error: () => {
        this.loading.set(false);
        this.failed.set(true);
      },
    });
  }

  /**
   * Danh mục provider: lỗi thì để rỗng — form tạo mới sẽ không cho chọn gì
   * thay vì làm hỏng luôn danh sách kết nối đang xem.
   */
  private loadCatalog(): void {
    this.llm.catalog().subscribe({
      next: (list) => this.catalog.set(list),
      error: () => this.catalog.set([]),
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
  return { label: '', provider: '', api_key: '', model: '' };
}

/** Bản nháp form "Thêm kết nối" — chưa chọn provider nào. */
function defaultCreateDraft(): MyProviderDraft {
  return emptyDraft();
}

/**
 * Chỉ gửi field có giá trị: key rỗng = giữ key đang có, model rỗng = giữ model
 * đang có, nên sửa một ô không vô tình xoá danh sách model đã tải.
 */
function toForm(draft: MyProviderDraft): LlmUserProviderForm {
  return compactForm({
    label: draft.label.trim(),
    provider: draft.provider.trim(),
    api_key: draft.api_key.trim(),
    model: draft.model.trim(),
  });
}
