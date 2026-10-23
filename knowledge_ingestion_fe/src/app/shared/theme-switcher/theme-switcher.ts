import { Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatMenuModule } from '@angular/material/menu';
import { THEME_PALETTES, ThemeService } from '../../core/theme/theme.service';

@Component({
  selector: 'app-theme-switcher',
  imports: [MatButtonModule, MatIconModule, MatMenuModule],
  templateUrl: './theme-switcher.html',
  styleUrl: './theme-switcher.scss'
})
export class ThemeSwitcher {
  protected readonly theme = inject(ThemeService);
  protected readonly palettes = THEME_PALETTES;
}
