import { DatePipe } from '@angular/common';
import { Component, inject, signal } from '@angular/core';
import { FormBuilder, ReactiveFormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { MatTableModule } from '@angular/material/table';
import { MatTabsModule } from '@angular/material/tabs';
import { MatTooltipModule } from '@angular/material/tooltip';
import { filter } from 'rxjs';
import { AdminService } from '../../core/api/admin.service';
import { CategoriesService } from '../../core/api/categories.service';
import { ApiRequestError } from '../../core/api/api.helpers';
import {
  CategoryNode,
  ConsistencyCheckResponse,
  EmbedCacheStatsResponse,
  PurgeItem,
  QueuePhaseCounts,
  QueueStatusResponse,
  ResetScopeRequest,
  ResetScopeResponse,
} from '../../core/api/api.models';
import { MessageService } from '../../shared/feedback/message.service';
import { LangService } from '../../shared/i18n/lang.service';
import { TranslationKey } from '../../shared/i18n/lang.model';

const QUEUE_LIMIT = 20;

/**
 * Trang admin (chỉ role >= 1 thấy nav — BE vẫn gate 403):
 * hàng đợi extract/index + purge, kiểm tra nhất quán, thống kê/purge embed cache,
 * và thao tác lô rebuild-vectors / reset-extract theo scope (article_ids hoặc category_id).
 */
@Component({
  selector: 'app-admin',
  imports: [
    DatePipe,
    ReactiveFormsModule,
    MatButtonModule,
    MatCardModule,
    MatChipsModule,
    MatFormFieldModule,
    MatIconModule,
    MatInputModule,
    MatSelectModule,
    MatTableModule,
    MatTabsModule,
    MatTooltipModule,
  ],
  templateUrl: './admin-component.html',
  styleUrl: './admin-component.scss',
})
export class AdminComponent {
  private readonly adminApi = inject(AdminService);
  private readonly categoriesApi = inject(CategoriesService);
  private readonly messages = inject(MessageService);
  private readonly fb = inject(FormBuilder);
  protected readonly lang = inject(LangService);

  /* -------- Queue tab -------- */
  protected readonly queue = signal<QueueStatusResponse | null>(null);
  protected readonly queueLoading = signal(false);
  protected readonly queueColumns: string[] = ['name', 'attempts', 'nextRetry', 'lease', 'running', 'flags'];
  protected readonly purgeColumns: string[] = ['name', 'waiting', 'undoUntil', 'statuses'];

  /* -------- Consistency tab -------- */
  protected readonly consistency = signal<ConsistencyCheckResponse | null>(null);
  protected readonly consistencyLoading = signal(false);

  /* -------- Embed cache tab -------- */
  protected readonly embedStats = signal<EmbedCacheStatsResponse | null>(null);
  protected readonly purgeForm = this.fb.nonNullable.group({ olderThanDays: [0] });

  /* -------- Maintenance tab -------- */
  protected readonly categoryOptions = signal<{ id: number; label: string }[]>([]);
  protected readonly scopeForm = this.fb.nonNullable.group({ categoryId: [0], articleIds: [''] });

  constructor() {
    this.loadQueue();
    this.loadEmbedStats();
    this.categoriesApi.list().subscribe({
      next: (res) => {
        this.categoryOptions.set(this.orderTree(res.categories).map((n) => ({ id: n.id, label: `${'\u00A0\u00A0'.repeat(n.depth)}${n.name}` })));
      },
      error: (err: unknown) => this.showError(err),
    });
  }

  /* ============================== Queue ============================== */

  protected phaseNames(): ('extract' | 'index')[] {
    return ['extract', 'index'];
  }

  protected groupNames(): ('processing' | 'failed' | 'pending')[] {
    return ['processing', 'failed', 'pending'];
  }

  protected purgeGroupNames(): ('undoing' | 'ready_to_purge')[] {
    return ['undoing', 'ready_to_purge'];
  }

  protected loadQueue(): void {
    this.queueLoading.set(true);
    this.adminApi.queue(QUEUE_LIMIT).subscribe({
      next: (res) => {
        this.queue.set(res);
        this.queueLoading.set(false);
      },
      error: (err: unknown) => {
        this.queueLoading.set(false);
        this.showError(err);
      },
    });
  }

  /** Chuỗi đếm 4 trạng thái hàng đợi: pending / processing / done / failed. */
  protected countsLabel(phase: QueuePhaseCounts): string {
    return `${phase.pending} / ${phase.processing} / ${phase.done} / ${phase.failed}`;
  }

  /* ============================== Consistency ============================== */

  protected runConsistency(): void {
    this.consistencyLoading.set(true);
    this.adminApi.consistency().subscribe({
      next: (res) => {
        this.consistency.set(res);
        this.consistencyLoading.set(false);
        if (res.healthy) {
          this.messages.success('ADMIN_CONSISTENCY_HEALTHY');
        } else {
          this.messages.warning('ADMIN_CONSISTENCY_ISSUES');
        }
      },
      error: (err: unknown) => {
        this.consistencyLoading.set(false);
        this.showError(err);
      },
    });
  }

  /* ============================== Embed cache ============================== */

  protected loadEmbedStats(): void {
    this.adminApi.embedCacheStats().subscribe({
      next: (res) => this.embedStats.set(res),
      error: (err: unknown) => this.showError(err),
    });
  }

  protected purgeEmbedCache(): void {
    const days = this.purgeForm.getRawValue().olderThanDays;
    this.messages
      .confirm('COMMON_CONFIRM', 'ADMIN_PURGE_CACHE_CONFIRM', { days: days > 0 ? days : 'all' })
      .pipe(filter(Boolean))
      .subscribe(() => {
        const req = days > 0 ? { older_than_days: days } : {};
        this.adminApi.embedCachePurge(req).subscribe({
          next: (res) => {
            this.messages.raw('success', this.lang.t('ADMIN_PURGE_CACHE_DONE', { count: res.deleted }));
            this.loadEmbedStats();
          },
          error: (err: unknown) => this.showError(err),
        });
      });
  }

  formatBytes(bytes: number): string {
    if (bytes < 1024) {
      return `${bytes} B`;
    }
    const units = ['KB', 'MB', 'GB'];
    let value = bytes;
    let unit = 'B';
    for (const u of units) {
      value /= 1024;
      unit = u;
      if (value < 1024) {
        break;
      }
    }
    return `${value.toFixed(1)} ${unit}`;
  }

  /* ============================== Maintenance (scope lô) ============================== */

  /** Scope từ form: article_ids (CSV) ưu tiên, nếu không thì category_id; trống cả hai = null. */
  private buildScope(): ResetScopeRequest | null {
    const v = this.scopeForm.getRawValue();
    const ids = v.articleIds
      .split(/[,\s]+/)
      .map((s) => Number(s))
      .filter((n) => Number.isInteger(n) && n > 0);
    if (ids.length > 0) {
      return { article_ids: ids };
    }
    if (v.categoryId > 0) {
      return { category_id: v.categoryId };
    }
    return null;
  }

  protected rebuildVectors(): void {
    const scope = this.buildScope();
    if (!scope) {
      this.messages.error('ADMIN_SCOPE_REQUIRED');
      return;
    }
    this.messages
      .confirm('COMMON_CONFIRM', 'ADMIN_REBUILD_CONFIRM')
      .pipe(filter(Boolean))
      .subscribe(() => {
        this.adminApi.rebuildVectors(scope).subscribe({
          next: (res) => this.reportScopeResult('ADMIN_REBUILD_DONE', res),
          error: (err: unknown) => this.showError(err),
        });
      });
  }

  protected resetExtract(): void {
    const scope = this.buildScope();
    if (!scope) {
      this.messages.error('ADMIN_SCOPE_REQUIRED');
      return;
    }
    this.messages
      .confirm('COMMON_CONFIRM', 'ADMIN_RESET_EXTRACT_CONFIRM')
      .pipe(filter(Boolean))
      .subscribe(() => {
        this.adminApi.resetExtract(scope).subscribe({
          next: (res) => this.reportScopeResult('ADMIN_RESET_EXTRACT_DONE', res),
          error: (err: unknown) => this.showError(err),
        });
      });
  }

  private reportScopeResult(key: TranslationKey, res: ResetScopeResponse): void {
    this.messages.raw('success', this.lang.t(key, { count: res.reset_articles }));
    this.loadQueue();
  }

  protected purgeItemFlags(item: PurgeItem): string {
    return item.can_undo ? this.lang.t('ADMIN_PURGE_CAN_UNDO') : this.lang.t('ADMIN_PURGE_WAITING');
  }

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
