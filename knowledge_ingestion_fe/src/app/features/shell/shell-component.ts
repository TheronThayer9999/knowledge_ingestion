import { Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatIcon } from '@angular/material/icon';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { AuthService } from '../../core/auth/services/auth.service';
import { TopBar } from '../../shared/components/top-bar/top-bar';
import { LangService } from '../../shared/i18n/lang.service';
import { TranslationKey } from '../../shared/i18n/lang.model';

interface NavItem {
  path: string;
  icon: string;
  labelKey: TranslationKey;
  /** true = chỉ hiện khi role >= 1 (admin) — gate UX, BE vẫn gate thật. */
  adminOnly?: boolean;
}

/**
 * Shell các trang cần đăng nhập: TopBar + hàng nav + router-outlet con.
 * Thêm trang mới = thêm item vào NAV (và route tương ứng trong app.routes).
 */
@Component({
  selector: 'app-shell',
  imports: [TopBar, RouterOutlet, RouterLink, RouterLinkActive, MatButtonModule, MatIcon],
  templateUrl: './shell-component.html',
  styleUrl: './shell-component.scss',
})
export class ShellComponent {
  protected readonly auth = inject(AuthService);
  protected readonly lang = inject(LangService);

  protected readonly nav: NavItem[] = [
    { path: '/dashboard', icon: 'dashboard', labelKey: 'NAV_DASHBOARD' },
    { path: '/categories', icon: 'folder', labelKey: 'NAV_CATEGORIES' },
    { path: '/articles', icon: 'article', labelKey: 'NAV_ARTICLES' },
    { path: '/search', icon: 'search', labelKey: 'NAV_SEARCH' },
    { path: '/chat', icon: 'chat', labelKey: 'NAV_CHAT' },
    { path: '/users', icon: 'groups', labelKey: 'NAV_USERS' },
    { path: '/profile', icon: 'person', labelKey: 'NAV_PROFILE' },
    { path: '/admin', icon: 'admin_panel_settings', labelKey: 'NAV_ADMIN', adminOnly: true },
  ];

  protected visibleNav(): NavItem[] {
    const isAdmin = (this.auth.user()?.role ?? 0) >= 1;
    return this.nav.filter((item) => !item.adminOnly || isAdmin);
  }
}
