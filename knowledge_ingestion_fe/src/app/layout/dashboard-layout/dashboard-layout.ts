import { BreakpointObserver } from '@angular/cdk/layout';
import { Component, computed, inject, signal } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { MatButtonModule } from '@angular/material/button';
import { MatDividerModule } from '@angular/material/divider';
import { MatIconModule } from '@angular/material/icon';
import { MatListModule } from '@angular/material/list';
import { MatMenuModule } from '@angular/material/menu';
import { MatSidenavModule } from '@angular/material/sidenav';
import { MatToolbarModule } from '@angular/material/toolbar';
import { MatTooltipModule } from '@angular/material/tooltip';
import { NavigationEnd, Router, RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { filter, map } from 'rxjs';
import { AuthService } from '../../core/auth/auth.service';
import { LangService, type TranslationKey } from '../../core/i18n/lang.service';
import { LanguageSwitcher } from '../../shared/language-switcher/language-switcher';
import { ThemeSwitcher } from '../../shared/theme-switcher/theme-switcher';

interface NavItem {
  path: string;
  icon: string;
  label: TranslationKey;
}

const WORKSPACE_NAV: NavItem[] = [
  { path: '/app/overview', icon: 'space_dashboard', label: 'NAV_OVERVIEW' },
  { path: '/app/documents', icon: 'folder_open', label: 'NAV_DOCUMENTS' },
  { path: '/app/chat', icon: 'forum', label: 'NAV_CHAT' },
];

const ACCOUNT_NAV: NavItem[] = [
  { path: '/app/settings', icon: 'settings', label: 'NAV_SETTINGS' },
  { path: '/app/profile', icon: 'account_circle', label: 'NAV_PROFILE' },
];

/**
 * Khung dashboard: sidenav dọc (thu gọn còn rail ở desktop, drawer ở mobile)
 * + topbar giữ theme/ngôn ngữ/tài khoản. Chỉ trang con đổi, khung giữ nguyên.
 */
@Component({
  selector: 'app-dashboard-layout',
  imports: [
    RouterOutlet,
    RouterLink,
    RouterLinkActive,
    MatSidenavModule,
    MatToolbarModule,
    MatListModule,
    MatIconModule,
    MatButtonModule,
    MatMenuModule,
    MatDividerModule,
    MatTooltipModule,
    LanguageSwitcher,
    ThemeSwitcher,
  ],
  templateUrl: './dashboard-layout.html',
  styleUrl: './dashboard-layout.scss',
})
export class DashboardLayout {
  protected readonly auth = inject(AuthService);
  protected readonly lang = inject(LangService);
  private readonly router = inject(Router);
  private readonly breakpoints = inject(BreakpointObserver);

  protected readonly workspaceNav = WORKSPACE_NAV;
  protected readonly accountNav = ACCOUNT_NAV;
  protected readonly collapsed = signal(false);
  protected readonly mobileOpen = signal(false);

  protected readonly isHandset = toSignal(
    this.breakpoints.observe('(max-width: 1023px)').pipe(map((state) => state.matches)),
    { initialValue: false },
  );

  private readonly navigationEnd = toSignal(
    this.router.events.pipe(filter((event) => event instanceof NavigationEnd)),
    { initialValue: null },
  );

  /** Tiêu đề topbar bám theo route đang mở — nguồn duy nhất là bảng nav */
  protected readonly pageTitle = computed<TranslationKey>(() => {
    this.navigationEnd();
    const url = this.router.url;
    const all = [...this.workspaceNav, ...this.accountNav];
    return all.find((item) => url.startsWith(item.path))?.label ?? 'NAV_OVERVIEW';
  });

  protected readonly initials = computed(() => {
    const name = this.auth.user()?.username ?? '';
    return name.slice(0, 2).toUpperCase();
  });

  protected readonly rail = computed(() => this.collapsed() && !this.isHandset());

  protected toggleNav(): void {
    if (this.isHandset()) {
      this.mobileOpen.update((open) => !open);
      return;
    }
    this.collapsed.update((value) => !value);
  }

  protected closeMobileNav(): void {
    if (this.isHandset()) {
      this.mobileOpen.set(false);
    }
  }

  protected logout(): void {
    this.auth.logout().subscribe({
      complete: () => void this.router.navigate(['/login']),
    });
  }
}
