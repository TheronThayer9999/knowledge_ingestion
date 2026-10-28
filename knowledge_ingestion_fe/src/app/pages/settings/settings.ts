import { Component, computed, inject, signal } from '@angular/core';
import { MatButtonToggleModule } from '@angular/material/button-toggle';
import { MatCardModule } from '@angular/material/card';
import { MatIconModule } from '@angular/material/icon';
import { AuthService } from '../../core/auth/auth.service';
import { LangService, type LangName } from '../../core/i18n/lang.service';
import type { LlmProviderOption } from '../../core/llm/llm.models';
import { LlmService } from '../../core/llm/llm.service';
import {
  THEME_PALETTES,
  ThemeService,
  type ThemeMode,
  type ThemePalette,
} from '../../core/theme/theme.service';

const PALETTE_KEYS: Record<ThemePalette, 'PALETTE_AZURE' | 'PALETTE_ROSE_RED' | 'PALETTE_MAGENTA_VIOLET' | 'PALETTE_CYAN_ORANGE'> = {
  azure: 'PALETTE_AZURE',
  'rose-red': 'PALETTE_ROSE_RED',
  'magenta-violet': 'PALETTE_MAGENTA_VIOLET',
  'cyan-orange': 'PALETTE_CYAN_ORANGE',
};

/**
 * Cài đặt giao diện: bảng màu + sáng/tối + ngôn ngữ. Mọi thay đổi áp và
 * lưu ngay (ThemeService/LangService tự persist), không cần nút "Lưu".
 *
 * Thêm card "Nhà cung cấp AI" (Đợt 28) — CHỈ ADMIN thấy. Card này CHỈ ĐỌC:
 * API key nằm trong `configs/config.json` nên UI không có ô nhập key (Nấc 2 mới
 * có bảng DB + CRUD). Endpoint `/llm/options` cũng đã lọc: user thường không
 * nhận được `base_url`/`has_key`, nên card này với user thường sẽ trống — vì
 * vậy phải chặn ở tầng template chứ không dựa vào dữ liệu API.
 */
@Component({
  selector: 'app-settings',
  imports: [MatCardModule, MatIconModule, MatButtonToggleModule],
  templateUrl: './settings.html',
  styleUrl: './settings.scss',
})
export class Settings {
  protected readonly lang = inject(LangService);
  protected readonly theme = inject(ThemeService);
  protected readonly auth = inject(AuthService);
  private readonly llm = inject(LlmService);
  protected readonly palettes = THEME_PALETTES;

  /** Danh sách provider đã cấu hình — chỉ admin mới nạp */
  protected readonly providers = signal<LlmProviderOption[]>([]);
  protected readonly llmLoading = signal(false);
  protected readonly llmFailed = signal(false);

  /** Chỉ admin thấy card nhà cung cấp AI */
  protected readonly showLlm = computed(() => this.auth.isAdmin());

  constructor() {
    if (this.auth.isAdmin()) {
      this.loadProviders();
    }
  }

  /**
   * Nạp danh sách kết nối. Ở đây KHÔNG lọc `available` — provider chưa dựng
   * được agent (thiếu key) vẫn phải hiện, đó chính là thứ admin cần thấy để
   * biết cấu hình sai ở đâu.
   */
  private loadProviders(): void {
    this.llmLoading.set(true);
    this.llmFailed.set(false);
    this.llm.options().subscribe({
      next: (options) => {
        this.llmLoading.set(false);
        this.providers.set(options.providers);
      },
      error: () => {
        this.llmLoading.set(false);
        this.llmFailed.set(true);
      },
    });
  }

  protected paletteKey(palette: ThemePalette): string {
    return this.lang.t(PALETTE_KEYS[palette]);
  }

  protected setMode(mode: ThemeMode): void {
    this.theme.setMode(mode);
  }

  protected setLang(lang: LangName): void {
    this.lang.setLang(lang);
  }
}
