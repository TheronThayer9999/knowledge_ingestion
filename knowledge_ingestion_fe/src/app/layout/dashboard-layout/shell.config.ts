import { type Type } from '@angular/core';
import { LanguageSwitcher } from '../../shared/language-switcher/language-switcher';
import { ThemeSwitcher } from '../../shared/theme-switcher/theme-switcher';
import type { TranslationKey } from '../../core/i18n/lang.service';

/**
 * Cấu hình APP SHELL — nguồn duy nhất cho sidebar và header của DashboardLayout.
 * Thêm mục menu = thêm 1 dòng vào NAV_GROUPS. Thêm action trên header (nút dịch,
 * theme, ...) = thêm component vào HEADER_ACTIONS. KHÔNG sửa template/JSX của
 * từng trang và không viết cứng danh sách menu trong shell.
 */

/** 1 mục điều hướng — path tuyệt đối của route con trong shell */
export interface NavItem {
  path: string;
  icon: string;
  label: TranslationKey;
}

/** 1 nhóm menu kèm tiêu đề nhóm (hiện 12px xám trên sidebar) */
export interface NavGroup {
  title: TranslationKey;
  items: NavItem[];
}

export const NAV_GROUPS: NavGroup[] = [
  {
    title: 'NAV_SECTION_WORKSPACE',
    items: [
      { path: '/app/overview', icon: 'space_dashboard', label: 'NAV_OVERVIEW' },
      { path: '/app/documents', icon: 'folder_open', label: 'NAV_DOCUMENTS' },
      { path: '/app/scan-jobs', icon: 'picture_as_pdf', label: 'NAV_SCAN_JOBS' },
      { path: '/app/chat', icon: 'forum', label: 'NAV_CHAT' },
    ],
  },
  {
    title: 'NAV_SECTION_ACCOUNT',
    items: [
      { path: '/app/settings', icon: 'settings', label: 'NAV_SETTINGS' },
      { path: '/app/profile', icon: 'account_circle', label: 'NAV_PROFILE' },
    ],
  },
];

/**
 * Action góc phải header — render qua ngComponentOutlet theo thứ tự mảng.
 * Type<unknown>[] vì ngComponentOutlet Angular 21 suy generic chặt theo phần
 * tử — để union component không break type-check của template.
 */
export const HEADER_ACTIONS: Type<unknown>[] = [LanguageSwitcher, ThemeSwitcher];

/** Title fallback khi URL không khớp mục nav nào (vd route con tương lai) */
export const DEFAULT_PAGE_TITLE: TranslationKey = NAV_GROUPS[0].items[0].label;
