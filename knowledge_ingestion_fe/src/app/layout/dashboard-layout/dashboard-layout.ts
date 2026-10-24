import { BreakpointObserver } from '@angular/cdk/layout';
import { NgComponentOutlet } from '@angular/common';
import {
  Component,
  DestroyRef,
  ElementRef,
  afterNextRender,
  computed,
  inject,
  signal,
  viewChild,
} from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { MatButtonModule } from '@angular/material/button';
import { MatDividerModule } from '@angular/material/divider';
import { MatIconModule } from '@angular/material/icon';
import { MatListModule } from '@angular/material/list';
import { MatMenuModule } from '@angular/material/menu';
import { MatSidenavContainer, MatSidenavModule } from '@angular/material/sidenav';
import { MatToolbarModule } from '@angular/material/toolbar';
import { MatTooltipModule } from '@angular/material/tooltip';
import { NavigationEnd, Router, RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { filter, map } from 'rxjs';
import { AuthService } from '../../core/auth/auth.service';
import { LangService, type TranslationKey } from '../../core/i18n/lang.service';
import {
  DEFAULT_PAGE_TITLE,
  HEADER_ACTIONS,
  NAV_GROUPS,
  type NavGroup,
} from './shell.config';

/**
 * APP SHELL duy nhất của vùng đăng nhập: sidenav dọc (thu gọn còn rail ở
 * desktop, drawer ở mobile) + topbar giữ theme/ngôn ngữ/tài khoản + vùng
 * content. Sidebar/header render hoàn toàn từ shell.config.ts (NAV_GROUPS,
 * HEADER_ACTIONS) — thêm menu/action chỉ sửa config, không sửa file này.
 * Chỉ trang con đổi, khung giữ nguyên.
 */
@Component({
  selector: 'app-dashboard-layout',
  imports: [
    RouterOutlet,
    RouterLink,
    RouterLinkActive,
    NgComponentOutlet,
    MatSidenavModule,
    MatToolbarModule,
    MatListModule,
    MatIconModule,
    MatButtonModule,
    MatMenuModule,
    MatDividerModule,
    MatTooltipModule,
  ],
  templateUrl: './dashboard-layout.html',
  styleUrl: './dashboard-layout.scss',
})
export class DashboardLayout {
  protected readonly auth = inject(AuthService);
  protected readonly lang = inject(LangService);
  private readonly router = inject(Router);
  private readonly breakpoints = inject(BreakpointObserver);

  protected readonly navGroups: NavGroup[] = NAV_GROUPS;
  protected readonly headerActions = HEADER_ACTIONS;
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

  /** Tiêu đề topbar bám theo route đang mở — nguồn duy nhất là bảng nav config */
  protected readonly pageTitle = computed<TranslationKey>(() => {
    this.navigationEnd();
    const url = this.router.url;
    const all = this.navGroups.flatMap((group) => group.items);
    return all.find((item) => url.startsWith(item.path))?.label ?? DEFAULT_PAGE_TITLE;
  });

  protected readonly initials = computed(() => {
    const name = this.auth.user()?.username ?? '';
    return name.slice(0, 2).toUpperCase();
  });

  protected readonly rail = computed(() => this.collapsed() && !this.isHandset());

  private readonly shell = viewChild(MatSidenavContainer);
  // `#nav` nằm trên component nên ref mặc định trỏ tới instance MatSidenav
  // (không có nativeElement công khai) — phải đọc ElementRef tường minh,
  // nếu không observer âm thầm không attach và content không kéo theo rail.
  private readonly navElement = viewChild('nav', { read: ElementRef });
  private readonly destroyRef = inject(DestroyRef);

  constructor() {
    // MatSidenav chỉ đo width của drawer lúc mở/đóng hoặc resize cửa sổ; thu
    // rail bằng class đổi mỗi width nên nó không đo lại, nội dung giữ margin
    // cũ và mất khoảng trống vừa giải phóng. Bám kích thước thật của drawer
    // để ép container tính lại margin (kể cả giữa lúc transition).
    afterNextRender(() => this.watchNavWidth());
  }

  private watchNavWidth(): void {
    const drawer = this.navElement()?.nativeElement;
    const shell = this.shell();
    if (!drawer || !shell) {
      return;
    }
    const observer = new ResizeObserver(() => shell.updateContentMargins());
    observer.observe(drawer);
    this.destroyRef.onDestroy(() => observer.disconnect());
  }

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
