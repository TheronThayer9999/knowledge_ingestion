import { DatePipe } from '@angular/common';
import { Component, computed, inject, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatIconModule } from '@angular/material/icon';
import { Router, RouterLink } from '@angular/router';
import { forkJoin } from 'rxjs';
import { AuthService } from '../../core/auth/auth.service';
import { LangService, type TranslationKey } from '../../core/i18n/lang.service';
import {
  docTypeKey as docTypeTranslationKey,
  type Article,
  type Category,
} from '../../core/knowledge/knowledge.models';
import { KnowledgeService } from '../../core/knowledge/knowledge.service';
import { MessageService } from '../../shared/feedback/message.service';

interface StatCard {
  icon: string;
  label: TranslationKey;
  value: number;
  hint?: TranslationKey;
}

const RECENT_LIMIT = 5;
const RECENT_DAYS = 7;

/** Tổng quan: KPI kho tri thức + tài liệu gần đây + lối tắt thao tác */
@Component({
  selector: 'app-overview',
  imports: [RouterLink, MatCardModule, MatIconModule, MatButtonModule, DatePipe],
  templateUrl: './overview.html',
  styleUrl: './overview.scss',
})
export class Overview {
  protected readonly auth = inject(AuthService);
  protected readonly lang = inject(LangService);
  private readonly knowledge = inject(KnowledgeService);
  private readonly messages = inject(MessageService);
  private readonly router = inject(Router);

  protected readonly articles = signal<Article[]>([]);
  protected readonly categories = signal<Category[]>([]);
  protected readonly total = signal(0);
  protected readonly loading = signal(true);
  protected readonly failed = signal(false);

  protected readonly stats = computed<StatCard[]>(() => {
    const articles = this.articles();
    const types = new Set(articles.map((article) => article.docType).filter(Boolean));
    return [
      {
        icon: 'description',
        label: 'DASH_STAT_DOCUMENTS',
        value: this.total(),
      },
      {
        icon: 'folder',
        label: 'DASH_STAT_FOLDERS',
        value: this.categories().length,
      },
      {
        icon: 'category',
        label: 'DASH_STAT_TYPES',
        value: types.size,
      },
      {
        icon: 'update',
        label: 'DASH_STAT_UPDATED',
        value: this.recentlyUpdatedCount(),
        hint: 'DASH_STAT_UPDATED_HINT',
      },
    ];
  });

  protected readonly recent = computed(() =>
    [...this.articles()]
      .sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt))
      .slice(0, RECENT_LIMIT),
  );

  constructor() {
    this.load();
  }

  protected load(): void {
    this.loading.set(true);
    this.failed.set(false);
    forkJoin({
      articles: this.knowledge.listAllArticles(),
      categories: this.knowledge.listCategories(),
    }).subscribe({
      next: ({ articles, categories }) => {
        this.articles.set(articles);
        this.total.set(articles.length);
        this.categories.set(categories);
        this.loading.set(false);
      },
      error: (error: unknown) => {
        this.loading.set(false);
        this.failed.set(true);
        this.messages.errorFromApi(error, 'DASH_LOAD_ERROR');
      },
    });
  }

  protected openUpload(): void {
    void this.router.navigate(['/app/documents'], { queryParams: { upload: 1 } });
  }

  protected openNewFolder(): void {
    void this.router.navigate(['/app/documents'], { queryParams: { newFolder: 1 } });
  }

  protected categoryName(id: number): string {
    return this.categories().find((category) => category.id === id)?.name ?? '';
  }

  protected docTypeKey(docType: string): TranslationKey {
    return docTypeTranslationKey(docType);
  }

  private recentlyUpdatedCount(): number {
    const threshold = Date.now() - RECENT_DAYS * 24 * 60 * 60 * 1000;
    return this.articles().filter((article) => Date.parse(article.updatedAt) >= threshold).length;
  }
}
