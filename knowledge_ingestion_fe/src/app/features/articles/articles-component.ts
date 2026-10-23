import { DatePipe } from '@angular/common';
import { Component, computed, inject, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatDialog } from '@angular/material/dialog';
import { MatIconModule } from '@angular/material/icon';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatSelectModule } from '@angular/material/select';
import { MatTableModule } from '@angular/material/table';
import { MatTooltipModule } from '@angular/material/tooltip';
import { ActivatedRoute, Router, RouterLink } from '@angular/router';
import { filter } from 'rxjs';
import { ArticlesService } from '../../core/api/articles.service';
import { CategoriesService } from '../../core/api/categories.service';
import { ApiRequestError } from '../../core/api/api.helpers';
import { Article, CategoryNode } from '../../core/api/api.models';
import { MessageService } from '../../shared/feedback/message.service';
import { LangService } from '../../shared/i18n/lang.service';
import { TranslationKey } from '../../shared/i18n/lang.model';
import { ArticleCreateDialog } from './article-create-dialog';

const PAGE_SIZE = 20;

/**
 * Trang tài liệu: BE bắt list theo ĐÚNG 1 danh mục (không có "tất cả"),
 * nên UI chọn category trước — mặc định category đầu tiên, chưa có danh mục
 * thì hướng dẫn tạo trước.
 */
@Component({
  selector: 'app-articles',
  imports: [
    DatePipe,
    MatButtonModule,
    MatCardModule,
    MatFormFieldModule,
    MatIconModule,
    MatSelectModule,
    MatTableModule,
    MatTooltipModule,
    RouterLink,
  ],
  templateUrl: './articles-component.html',
  styleUrl: './articles-component.scss',
})
export class ArticlesComponent {
  private readonly articlesApi = inject(ArticlesService);
  private readonly categoriesApi = inject(CategoriesService);
  private readonly dialog = inject(MatDialog);
  private readonly router = inject(Router);
  private readonly route = inject(ActivatedRoute);
  private readonly messages = inject(MessageService);
  protected readonly lang = inject(LangService);

  protected readonly columns: string[] = ['name', 'docType', 'source', 'updatedAt', 'actions'];
  protected readonly categories = signal<CategoryNode[]>([]);
  protected readonly articles = signal<Article[]>([]);
  protected readonly total = signal(0);
  protected readonly loading = signal(true);
  protected readonly selectedCategory = signal<number>(0);
  protected readonly pageIndex = signal(0);

  /** Thứ tự cây + nhãn thụt lề cho select danh mục. */
  protected readonly orderedCategories = computed<CategoryNode[]>(() => {
    const list = this.categories();
    const byParent = new Map<number | null, CategoryNode[]>();
    for (const n of list) {
      const key = n.parent_id ?? null;
      const bucket = byParent.get(key) ?? [];
      bucket.push(n);
      byParent.set(key, bucket);
    }
    const out: CategoryNode[] = [];
    const walk = (parent: number | null): void => {
      for (const n of byParent.get(parent) ?? []) {
        out.push(n);
        walk(n.id);
      }
    };
    walk(null);
    return out;
  });

  protected readonly categoryLabel = computed<string>(() => {
    const id = this.selectedCategory();
    const node = this.categories().find((c) => c.id === id);
    return node ? node.name : '';
  });

  constructor() {
    // ?category_id= từ trang categories (click folder) — hoặc tham số đầu vào.
    this.categoriesApi.list().subscribe({
      next: (res) => {
        this.categories.set(res.categories);
        const fromQuery = Number(this.route.snapshot.queryParamMap.get('category_id') ?? 0);
        const first = this.orderedCategories()[0]?.id ?? 0;
        this.selectedCategory.set(fromQuery > 0 ? fromQuery : first);
        if (this.selectedCategory() > 0) {
          this.loadArticles();
        } else {
          this.loading.set(false);
        }
      },
      error: (err: unknown) => {
        this.loading.set(false);
        this.showError(err);
      },
    });
  }

  protected selectCategory(id: number): void {
    this.selectedCategory.set(id);
    this.pageIndex.set(0);
    this.loadArticles();
  }

  protected nextPage(): void {
    this.pageIndex.update((p) => p + 1);
    this.loadArticles();
  }

  protected prevPage(): void {
    this.pageIndex.update((p) => Math.max(0, p - 1));
    this.loadArticles();
  }

  protected openCreate(): void {
    const candidates = this.orderedCategories().map((n) => ({
      id: n.id,
      label: `${'\u00A0\u00A0'.repeat(n.depth)}${n.name}`,
    }));
    const ref = this.dialog.open(ArticleCreateDialog, { data: { candidates }, width: '480px' });
    ref.afterClosed()
      .pipe(filter((a): a is Article => a !== undefined))
      .subscribe(() => {
        this.messages.success('ARTICLES_CREATED');
        this.loadArticles();
      });
  }

  protected open(article: Article): void {
    void this.router.navigate(['/articles', article.id]);
  }

  protected remove(article: Article): void {
    this.messages
      .confirm('COMMON_CONFIRM', 'ARTICLES_DELETE_CONFIRM', { name: article.name })
      .pipe(filter(Boolean))
      .subscribe(() => {
        this.articlesApi.remove(article.id).subscribe({
          next: () => {
            this.messages.success('ARTICLES_DELETED');
            this.loadArticles();
          },
          error: (err: unknown) => this.showError(err),
        });
      });
  }

  protected sourceLabel(article: Article): TranslationKey {
    return article.url ? 'ARTICLES_SOURCE_URL' : 'ARTICLES_SOURCE_FILE';
  }

  protected hasMore(): boolean {
    return (this.pageIndex() + 1) * PAGE_SIZE < this.total();
  }

  protected pageSize = PAGE_SIZE;

  private loadArticles(): void {
    this.loading.set(true);
    this.articlesApi
      .list({ category_id: this.selectedCategory(), limit: PAGE_SIZE, offset: this.pageIndex() * PAGE_SIZE })
      .subscribe({
        next: (res) => {
          this.articles.set(res.articles);
          this.total.set(res.total);
          this.loading.set(false);
        },
        error: (err: unknown) => {
          this.loading.set(false);
          this.showError(err);
        },
      });
  }

  private showError(err: unknown): void {
    if (err instanceof ApiRequestError) {
      this.messages.raw('error', err.message);
    } else {
      this.messages.error('LOGIN_ERRORS_GENERIC');
    }
  }
}
