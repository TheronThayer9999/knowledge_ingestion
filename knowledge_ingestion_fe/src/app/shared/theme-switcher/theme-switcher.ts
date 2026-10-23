import { Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatMenuModule } from '@angular/material/menu';
import { LangService, TranslationKey } from '../../core/i18n/lang.service';
import { THEME_PALETTES, ThemePalette, ThemeService } from '../../core/theme/theme.service';

const PALETTE_KEYS: Record<ThemePalette, TranslationKey> = {
  azure: 'PALETTE_AZURE',
  'rose-red': 'PALETTE_ROSE_RED',
  'magenta-violet': 'PALETTE_MAGENTA_VIOLET',
  'cyan-orange': 'PALETTE_CYAN_ORANGE',
};

@Component({
  selector: 'app-theme-switcher',
  imports: [MatButtonModule, MatIconModule, MatMenuModule],
  templateUrl: './theme-switcher.html',
  styleUrl: './theme-switcher.scss',
})
export class ThemeSwitcher {
  protected readonly theme = inject(ThemeService);
  protected readonly lang = inject(LangService);
  protected readonly palettes = THEME_PALETTES;

  protected paletteKey(palette: ThemePalette): TranslationKey {
    return PALETTE_KEYS[palette];
  }

  protected modeLabelKey(): 'TOOLBAR_SWITCH_TO_LIGHT' | 'TOOLBAR_SWITCH_TO_DARK' {
    return this.theme.mode() === 'dark' ? 'TOOLBAR_SWITCH_TO_LIGHT' : 'TOOLBAR_SWITCH_TO_DARK';
  }
}
