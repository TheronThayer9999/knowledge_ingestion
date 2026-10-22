import { Component, inject } from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { MatIcon } from '@angular/material/icon';
import { MatTableModule } from '@angular/material/table';
import { AuthService } from '../../core/auth/services/auth.service';
import { LangService } from '../../shared/i18n/lang.service';
import { TranslationKey } from '../../shared/i18n/lang.model';

interface StatCard {
  icon: string;
  /** key i18n của nhãn thống kê */
  labelKey: TranslationKey;
  value: string;
}

interface RecentArticle {
  title: string;
  category: string;
  status: string;
  indexedAt: string;
}

type RecentColumn = keyof RecentArticle;

/** Tên cột bảng → key i18n (SCREAMING_SNAKE trong vi.json/en.json). */
const COLUMN_KEYS: Record<RecentColumn, TranslationKey> = {
  title: 'DASHBOARD_COLUMNS_TITLE',
  category: 'DASHBOARD_COLUMNS_CATEGORY',
  status: 'DASHBOARD_COLUMNS_STATUS',
  indexedAt: 'DASHBOARD_COLUMNS_INDEXED_AT',
};

/** Trang chủ sau đăng nhập: thống kê + bảng tài liệu gần đây (data dummy). */
@Component({
  selector: 'app-dashboard',
  imports: [MatCardModule, MatIcon, MatTableModule],
  templateUrl: './dashboard-component.html',
  styleUrl: './dashboard-component.scss',
})
export class DashboardComponent {
  protected readonly auth = inject(AuthService);
  protected readonly lang = inject(LangService);

  protected readonly stats: StatCard[] = [
    { icon: 'folder', labelKey: 'DASHBOARD_STATS_CATEGORIES', value: '12' },
    { icon: 'article', labelKey: 'DASHBOARD_STATS_ARTICLES', value: '348' },
    { icon: 'hub', labelKey: 'DASHBOARD_STATS_CHUNKS', value: '9.7k' },
  ];

  protected readonly columns: RecentColumn[] = ['title', 'category', 'status', 'indexedAt'];

  /** Header bảng tra key i18n theo cột. */
  protected columnKey(col: RecentColumn): TranslationKey {
    return COLUMN_KEYS[col];
  }

  protected readonly recentArticles: RecentArticle[] = [
    { title: 'Quy định nội bộ', category: 'HR', status: 'Indexed', indexedAt: '20/10/2026' },
    { title: 'Quy định bổ sung', category: 'HR', status: 'Indexing', indexedAt: '18/10/2026' },
    { title: 'Triết học Đông phương', category: 'Philosophy', status: 'Indexed', indexedAt: '15/10/2026' },
  ];
}
