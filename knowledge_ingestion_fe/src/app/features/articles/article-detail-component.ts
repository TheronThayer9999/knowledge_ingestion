import { DatePipe } from '@angular/common';
import { Component, inject, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatDialog } from '@angular/material/dialog';
import { MatIconModule } from '@angular/material/icon';
import { MatTabsModule } from '@angular/material/tabs';
import { MatTooltipModule } from '@angular/material/tooltip';
import { MatTableModule } from '@angular/material/table';
import { ActivatedRoute, Router } from '@angular/router';
import { filter, switchMap } from 'rxjs';
import { ArticlesService } from '../../core/api/articles.service';
import { CategoriesService } from '../../core/api/categories.service';
import { ApiRequestError } from '../../core/api/api.helpers';
import { Article, ArticleSectionsResponse, ArticleTreeResponse } from '../../core/api/api.models';
import { MessageService } from '../../shared/feedback/message.service';
import { LangService } from '../../shared/i18n/lang.service';
import { ArticleEditDialog } from './article-edit-dialog';
import { SectionTree } from './section-tree';

/**
 * Chi tiết tài liệu: metadata + PageIndex (sections) + cây mục (tree).
 * worker còn extract/index nên status lấy từ responses, không phải article.
 */
@Component({
  selector: 'app-article-detail',
  imports: [
    DatePipe,
    MatButtonModule,
    MatCardModule,
    MatChipsModule,
    MatIconModule,
    MatTabsModule,
    MatTableModule,
    MatTooltipModule,
    SectionTree,
  ],
  templateUrl: './article-detail-component.html',
  styleUrl: './article-detail-component.scss',
})
export class ArticleDetailComponent {
  private readonly articlesApi = inject(ArticlesService);
  private readonly categoriesApi = inject(CategoriesService);
  private readonly dialog = inject(MatDialog);
  private readonly router = inject(Router);
  private readonly route = inject(ActivatedRoute);
  private readonly messages = inject(MessageService);
  protected readonly lang = inject(LangService);

  protected readonly article = signal<Article | null>(null);
  protected readonly sections = signal<ArticleSectionsResponse | null>(null);
  protected readonly tree = signal<ArticleTreeResponse | null>(null);
  protected readonly categoryName = signal('');
  protected readonly sectionColumns = ['name', 'from', 'summary'];

  constructor() {
    this.route.paramMap
      .pipe(
        switchMap((params) => {
          const id = Number(params.get('id'));
          return this.articlesApi.getById(id);
        }),
      )
      .subscribe({
        next: (article) => {
          this.article.set(article);
          const id = article.id;
          // sections + tree đọc nền — fail một trong hai không chặn trang.
          this.articlesApi.sections(id).subscribe({
            next: (res) => this.sections.set(res),
            error: () => undefined,
          });
          this.articlesApi.tree(id).subscribe({
            next: (res) => this.tree.set(res),
            error: () => undefined,
          });
          this.categoriesApi.list().subscribe({
            next: (res) => {
              this.categoryName.set(res.categories.find((c) => c.id === article.category_id)?.name ?? `#${article.category_id}`);
            },
            error: () => undefined,
          });
        },
        error: (err: unknown) => this.showError(err),
      });
  }

  protected back(): void {
    void this.router.navigate(['/articles'], { queryParams: { category_id: this.article()?.category_id } });
  }

  protected openEdit(): void {
    const article = this.article();
    if (!article) {
      return;
    }
    this.categoriesApi.list().subscribe({
      next: (res) => {
        const candidates = res.categories.map((c) => ({
          id: c.id,
          label: `${'\u00A0\u00A0'.repeat(c.depth)}${c.name}`,
        }));
        const ref = this.dialog.open(ArticleEditDialog, { data: { article, candidates }, width: '460px' });
        ref.afterClosed()
          .pipe(filter((req): req is NonNullable<typeof req> => req !== undefined))
          .subscribe((req) => {
            this.articlesApi.update(article.id, req).subscribe({
              next: (updated) => {
                this.article.set(updated);
                this.messages.success('ARTICLES_UPDATED');
              },
              error: (err: unknown) => this.showError(err),
            });
          });
      },
      error: (err: unknown) => this.showError(err),
    });
  }

  protected remove(): void {
    const article = this.article();
    if (!article) {
      return;
    }
    this.messages
      .confirm('COMMON_CONFIRM', 'ARTICLES_DELETE_CONFIRM', { name: article.name })
      .pipe(filter(Boolean))
      .subscribe(() => {
        this.articlesApi.remove(article.id).subscribe({
          next: () => {
            this.messages.success('ARTICLES_DELETED');
            this.back();
          },
          error: (err: unknown) => this.showError(err),
        });
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
