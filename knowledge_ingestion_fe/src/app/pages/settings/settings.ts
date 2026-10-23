import { Component, inject } from '@angular/core';
import { MatButtonToggleModule } from '@angular/material/button-toggle';
import { MatCardModule } from '@angular/material/card';
import { MatIconModule } from '@angular/material/icon';
import { LangService, type LangName } from '../../core/i18n/lang.service';
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
  protected readonly palettes = THEME_PALETTES;

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
