import { Component, inject } from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { MatIcon } from '@angular/material/icon';
import { MatTableModule } from '@angular/material/table';
import { TopBar } from '../../shared/components/top-bar/top-bar';
import { AuthService } from '../../core/auth/services/auth.service';

interface StatCard {
  icon: string;
  label: string;
  value: string;
}

interface RecentArticle {
  title: string;
  category: string;
  status: string;
  indexedAt: string;
}

/** Trang chủ sau đăng nhập: thống kê + bảng tài liệu gần đây (data dummy). */
@Component({
  selector: 'app-dashboard',
  imports: [TopBar, MatCardModule, MatIcon, MatTableModule],
  templateUrl: './dashboard-component.html',
  styleUrl: './dashboard-component.scss',
})
export class DashboardComponent {
  protected readonly auth = inject(AuthService);

  protected readonly stats: StatCard[] = [
    { icon: 'folder', label: 'Danh mục', value: '12' },
    { icon: 'article', label: 'Tài liệu', value: '348' },
    { icon: 'hub', label: 'Chunk đã index', value: '9.7k' },
  ];

  protected readonly columns: (keyof RecentArticle)[] = ['title', 'category', 'status', 'indexedAt'];

  protected readonly recentArticles: RecentArticle[] = [
    { title: 'Quy định nội bộ', category: 'HR', status: 'Indexed', indexedAt: '20/10/2026' },
    { title: 'Quy định bổ sung', category: 'HR', status: 'Indexing', indexedAt: '18/10/2026' },
    { title: 'Triết học Đông phương', category: 'Philosophy', status: 'Indexed', indexedAt: '15/10/2026' },
  ];
}
