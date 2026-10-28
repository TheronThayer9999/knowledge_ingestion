import { ChangeDetectionStrategy, Component, computed, inject, input, output } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatMenuModule } from '@angular/material/menu';
import { MatTooltipModule } from '@angular/material/tooltip';
import type { LlmProviderOption } from '../../core/llm/llm.models';
import type { Category } from '../../core/knowledge/knowledge.models';
import { LangService, type TranslationKey } from '../../core/i18n/lang.service';

/** Lựa chọn provider/model — rỗng = theo cấu hình mặc định */
export interface ModelChoice {
  provider: string;
  model: string;
}

/**
 * Chip "Phạm vi" + "Suy luận sâu" + "Mô hình" + nút phiên mới, nằm ở đầu
 * khung chat (Đợt 27 nhóm 5, mở rộng ở Đợt 28).
 *
 * Khoá là KHOÁ MỞM: `aria-disabled` + màu mờ, chứ không `disabled` thật, vì
 * nút disabled không nhận mouse/focus nên tooltip "tạo phiên mới để đổi" sẽ
 * không bao giờ hiện — người dùng bị chặn mà không hiểu lý do. Vì vậy click
 * phải tự chặn trong handler: khoá rồi thì KHÔNG phát output (trang Chat sẽ
 * không nhận được gì để đổi phạm vi/thinking nhầm).
 *
 * Chip MÔ HÌNH thì KHÔNG khoá theo phiên: backend nhận provider/model mỗi
 * lượt và lưu per-message, nên đổi giữa hội thoại là hợp lệ (giống
 * Open WebUI). Chỉ ẩn đi khi backend không trả danh sách provider (lỗi tải
 * options) — chat vẫn chạy bằng mặc định cấu hình.
 */
@Component({
  selector: 'app-chat-context-chips',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [MatButtonModule, MatIconModule, MatMenuModule, MatTooltipModule],
  templateUrl: './chat-context-chips.html',
  styleUrl: './chat-context-chips.scss',
})
export class ChatContextChips {
  protected readonly lang = inject(LangService);

  /** id phạm vi đang chọn — 0 là toàn bộ kho */
  readonly scopeId = input.required<number>();
  readonly scopeLabel = input.required<string>();
  readonly thinking = input.required<boolean>();
  /** Phiên đã có tin nhắn thì hai lựa chọn chốt theo phiên */
  readonly locked = input.required<boolean>();
  /** Đang chờ đáp án: đổi giữa chừng là mất lượt đang bay */
  readonly busy = input(false);
  readonly categories = input<Category[]>([]);
  /** Provider sẵn sàng (backend đã dựng agent) — rỗng thì ẩn chip mô hình */
  readonly providers = input<LlmProviderOption[]>([]);
  /** Provider/model đang chọn; rỗng = theo cấu hình */
  readonly provider = input('');
  readonly model = input('');
  /**
   * Provider/model đang chọn có client suy luận sâu không. Mặc định true để
   * màn chưa có options (backend lỗi) không tự khoá bừa; khi false thì chip
   * hiện khoá kèm lý do — bật được rồi bị âm thầm bỏ còn hại hơn là tắt nút.
   */
  readonly thinkingSupported = input(true);

  readonly pickScope = output<number>();
  readonly toggleThinking = output<void>();
  readonly newSession = output<void>();
  readonly pickModel = output<ModelChoice>();

  /** Chỉ hiện chip khi backend có provider để chọn (options tải lỗi thì ẩn) */
  protected readonly hasProviders = computed(() => this.providers().length > 0);

  /** Nhãn chip: "Provider · model" khi đã chọn, "Theo cấu hình" khi để trống */
  protected readonly modelLabel = computed(() => {
    const slug = this.provider();
    if (!slug) {
      return this.lang.t('CHAT_MODEL_CONFIG');
    }
    const label = this.providers().find((item) => item.slug === slug)?.label ?? slug;
    const model = this.model();
    return model ? `${label} · ${model}` : label;
  });

  /** Đang chọn provider cụ thể (không phải "theo cấu hình") thì chip sáng lên */
  protected readonly modelSelected = computed(() => this.provider() !== '');

  /** Khoá vì phiên đã có lượt hoặc đang chờ đáp án — cả hai đều không cho đổi */
  protected isLocked(): boolean {
    return this.locked() || this.busy();
  }

  /**
   * Chip suy luận sâu: khoá khi phiên khoá/busy, hoặc khi model đang chọn không
   * có client "nghĩ". Ở trường hợp sau chỉ khoá chiều BẬT — phiên cũ có thể lưu
   * thinking=true rồi provider đổi sang loại không hỗ trợ, user vẫn phải tắt
   * được để khớp cái backend sẽ làm.
   */
  protected readonly thinkingBlocked = computed(
    () => this.isLocked() || (!this.thinkingSupported() && !this.thinking()),
  );

  /** Tooltip nói đúng lý do: model không nghĩ được thì không phải tại phiên */
  protected thinkingHintKey(): TranslationKey {
    if (!this.thinkingSupported()) {
      return 'CHAT_THINKING_UNSUPPORTED';
    }
    return this.locked() ? 'CHAT_CHIP_LOCKED_HINT' : 'CHAT_THINKING_HINT';
  }

  protected chooseScope(id: number): void {
    if (this.isLocked()) {
      return;
    }
    this.pickScope.emit(id);
  }

  protected toggleDeepThinking(): void {
    if (this.thinkingBlocked()) {
      return;
    }
    this.toggleThinking.emit();
  }

  /** Model của provider: danh sách khai báo, thiếu thì lấy model mặc định */
  protected modelsOf(provider: LlmProviderOption): string[] {
    if (provider.models.length > 0) {
      return provider.models;
    }
    return provider.defaultModel ? [provider.defaultModel] : [];
  }

  protected isModelSelected(slug: string, model: string): boolean {
    return this.provider() === slug && this.model() === model;
  }

  protected chooseModel(slug: string, model: string): void {
    this.pickModel.emit({ provider: slug, model });
  }
}
