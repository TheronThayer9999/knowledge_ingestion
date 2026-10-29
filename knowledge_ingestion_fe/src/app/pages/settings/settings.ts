import { Component, computed, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatButtonToggleModule } from '@angular/material/button-toggle';
import { MatCardModule } from '@angular/material/card';
import { MatDialog } from '@angular/material/dialog';
import { MatIconModule } from '@angular/material/icon';
import { AuthService } from '../../core/auth/auth.service';
import { LangService, type LangName } from '../../core/i18n/lang.service';
import {
  THEME_PALETTES,
  ThemeService,
  type ThemeMode,
  type ThemePalette,
} from '../../core/theme/theme.service';
import { LlmProvidersDialog } from './llm-providers-dialog';
import { MyLlmProvidersDialog } from './my-llm-providers-dialog';

const PALETTE_KEYS: Record<
  ThemePalette,
  'PALETTE_AZURE' | 'PALETTE_ROSE_RED' | 'PALETTE_MAGENTA_VIOLET' | 'PALETTE_CYAN_ORANGE'
> = {
  azure: 'PALETTE_AZURE',
  'rose-red': 'PALETTE_ROSE_RED',
  'magenta-violet': 'PALETTE_MAGENTA_VIOLET',
  'cyan-orange': 'PALETTE_CYAN_ORANGE',
};

/**
 * Cài đặt giao diện: bảng màu + sáng/tối + ngôn ngữ. Mọi thay đổi áp và
 * lưu ngay (ThemeService/LangService tự persist), không cần nút "Lưu".
 *
 * Card "Nhà cung cấp AI" (Đợt 28) CHỈ ADMIN thấy và chỉ là cửa vào popup
 * `LlmProvidersDialog` — provider khai báo trong `configs/config.json`, popup
 * lo key/model. Không nạp danh sách ở trang này để user thường không tốn một
 * lời gọi API vô ích; API cũng đã chặn quyền ghi bằng role.
 */
@Component({
  selector: 'app-settings',
  imports: [MatCardModule, MatIconModule, MatButtonModule, MatButtonToggleModule],
  templateUrl: './settings.html',
  styleUrl: './settings.scss',
})
export class Settings {
  protected readonly lang = inject(LangService);
  protected readonly theme = inject(ThemeService);
  protected readonly auth = inject(AuthService);
  protected readonly palettes = THEME_PALETTES;
  private readonly dialog = inject(MatDialog);

  /** Chỉ admin thấy mục nhà cung cấp AI */
  protected readonly showLlm = computed(() => this.auth.isAdmin());

  /** Mở popup quản trị key/model; chặn ngay cả khi nút lọt ra UI */
  protected manageProviders(): void {
    if (!this.showLlm()) {
      return;
    }
    this.dialog.open(LlmProvidersDialog, { maxWidth: '94vw', autoFocus: 'first-tabbable' });
  }

  /**
   * Mở popup kết nối RIÊNG của tôi (BYOK) — mọi tài khoản đều thấy card này,
   * vì mỗi người có thể cắm provider LLM của riêng mình (base URL + API key).
   */
  protected manageMyProviders(): void {
    this.dialog.open(MyLlmProvidersDialog, {
      maxWidth: '94vw',
      autoFocus: 'first-tabbable',
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
