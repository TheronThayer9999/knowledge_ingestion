import { Component, inject, signal } from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatIconModule } from '@angular/material/icon';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { RouterLink } from '@angular/router';
import { CategoriesService } from '../../core/api/categories.service';
import { SearchService } from '../../core/api/search.service';
import { ApiRequestError } from '../../core/api/api.helpers';
import { CategoryNode, SearchHit, SearchRequest } from '../../core/api/api.models';
import { MessageService } from '../../shared/feedback/message.service';
import { LangService } from '../../shared/i18n/lang.service';

const DEFAULT_LIMIT = 10;

/**
 * Trang tìm kiếm tri thức (RAG): query + lọc danh mục (nhiều cái) + limit/score_threshold.
 * Danh mục hiển thị theo thứ tự cây (thụt lề) vì BE lọc theo category_ids phẳng.
 */
@Component({
  selector: 'app-search',
  imports: [
    ReactiveFormsModule,
    MatButtonModule,
    MatCardModule,
    MatChipsModule,
    MatFormFieldModule,
    MatIconModule,
    MatInputModule,
    MatSelectModule,
    RouterLink,
  ],
  templateUrl: './search-component.html',
  styleUrl: './search-component.scss',
})
export class SearchComponent {
  private readonly searchApi = inject(SearchService);
  private readonly categoriesApi = inject(CategoriesService);
  private readonly messages = inject(MessageService);
  private readonly fb = inject(FormBuilder);
  protected readonly lang = inject(LangService);

  protected readonly form = this.fb.nonNullable.group({
    query: ['', [Validators.required, Validators.minLength(2)]],
    categoryIds: [[] as number[]],
    limit: [DEFAULT_LIMIT],
    scoreThreshold: [0],
  });

  protected readonly categoryOptions = signal<{ id: number; label: string }[]>([]);
  protected readonly hits = signal<SearchHit[]>([]);
  protected readonly total = signal(0);
  protected readonly questionType = signal('');
  protected readonly searched = signal(false);
  protected readonly loading = signal(false);

  constructor() {
    this.categoriesApi.list().subscribe({
      next: (res) => {
        this.categoryOptions.set(this.orderTree(res.categories).map((n) => ({ id: n.id, label: `${'\u00A0\u00A0'.repeat(n.depth)}${n.name}` })));
      },
      error: (err: unknown) => this.showError(err),
    });
  }

  protected run(): void {
    if (this.form.invalid || this.loading()) {
      return;
    }
    const v = this.form.getRawValue();
    const req: SearchRequest = { query: v.query.trim(), limit: v.limit || DEFAULT_LIMIT };
    if (v.categoryIds.length > 0) {
      req.category_ids = v.categoryIds;
    }
    if (v.scoreThreshold > 0) {
      req.score_threshold = v.scoreThreshold;
    }
    this.loading.set(true);
    this.searchApi.search(req).subscribe({
      next: (res) => {
        this.hits.set(res.hits);
        this.total.set(res.total);
        this.questionType.set(res.question_type);
        this.searched.set(true);
        this.loading.set(false);
      },
      error: (err: unknown) => {
        this.loading.set(false);
        this.showError(err);
      },
    });
  }

  score(hit: SearchHit): string {
    return hit.score.toFixed(3);
  }

  /** DFS từ root để option trong select theo thứ tự cây (giống trang articles). */
  private orderTree(list: CategoryNode[]): CategoryNode[] {
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
  }

  private showError(err: unknown): void {
    if (err instanceof ApiRequestError) {
      this.messages.raw('error', err.message);
    } else {
      this.messages.error('LOGIN_ERRORS_GENERIC');
    }
  }
}
