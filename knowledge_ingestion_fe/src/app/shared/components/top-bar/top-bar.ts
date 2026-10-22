import { Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatIcon } from '@angular/material/icon';
import { MatMenuModule } from '@angular/material/menu';
import { MatToolbarModule } from '@angular/material/toolbar';
import { Router } from '@angular/router';
import { AVAILABLE_THEMES, ThemeName } from '../../../../styles/tokens/theme.model';
import { AuthService } from '../../../core/auth/services/auth.service';
import { LangService } from '../../i18n/lang.service';
import { ThemeService } from '../../theme/theme.service';

/**
 * Thanh toolbar dùng chung các trang cần đăng nhập:
 * tiêu đề, switcher theme (light/dark/brand), avatar + nút logout.
 */
@Component({
  selector: 'app-top-bar',
  imports: [MatToolbarModule, MatButtonModule, MatIcon, MatMenuModule],
  templateUrl: './top-bar.html',
  styleUrl: './top-bar.scss',
})
export class TopBar {
  protected readonly auth = inject(AuthService);
  protected readonly themeService = inject(ThemeService);
  protected readonly lang = inject(LangService);
  protected readonly themes = AVAILABLE_THEMES;
  private readonly router = inject(Router);

  protected selectTheme(name: ThemeName): void {
    this.themeService.setTheme(name);
  }

  protected logout(): void {
    this.auth.logout();
    void this.router.navigate(['/login']);
  }
}
