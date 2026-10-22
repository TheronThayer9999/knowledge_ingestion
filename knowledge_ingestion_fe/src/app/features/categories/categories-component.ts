import { Component, computed, inject, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatDialog } from '@angular/material/dialog';
import { MatIcon } from '@angular/material/icon';
import { MatTooltipModule } from '@angular/material/tooltip';
import { filter } from 'rxjs';
import { CategoriesService } from '../../core/api/categories.service';
import { CategoryNode, UpdateCategoryRequest } from '../../core/api/api.models';
import { ApiRequestError } from '../../core/api/api.helpers';
import { MessageService } from '../../shared/feedback/message.service';
import { LangService } from '../../shared/i18n/lang.service';
import { CategoryDialog, CategoryDialogData, CategoryDialogResult } from './category-dialog';

/**
 * Trang danh mục: cây thư mục của riêng user (BE trả danh sách phẳng
 * kèm parent_id/depth — FE chỉ việc dựng thứ tự DFS + thụt lề).
 * CRUD đầy đủ: tạo root/con, sửa (đổi tên/mô tả/cha/move_to_root), xóa.
 */
@Component({
  selector: 'app-categories',
  imports: [MatButtonModule, MatCardModule, MatIcon, MatTooltipModule],
  templateUrl: './categories-component.html',
  styleUrl: './categories-component.scss',
})
export class CategoriesComponent {
  private readonly categoriesApi = inject(CategoriesService);
  private readonly dialog = inject(MatDialog);
  private readonly messages = inject(MessageService);
  protected readonly lang = inject(LangService);

  protected readonly nodes = signal<CategoryNode[]>([]);
  protected readonly loading = signal(true);

  /** Thứ tự cây: DFS từ các root, parent đứng trước toàn bộ con. */
  protected readonly ordered = computed<CategoryNode[]>(() => {
    const list = this.nodes();
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
    // Cycle trong dữ liệu không thể xảy ra vì BE chặn — nhưng nếu cây lệch
    // thì node mồ côi vẫn phải hiển thị: nối tiếp vào cuối danh sách.
    for (const n of list) {
      if (!out.includes(n)) {
        out.push(n);
      }
    }
    return out;
  });

  constructor() {
    this.reload();
  }

  protected reload(): void {
    this.loading.set(true);
    this.categoriesApi.list().subscribe({
      next: (res) => {
        this.nodes.set(res.categories);
        this.loading.set(false);
      },
      error: (err: unknown) => {
        this.loading.set(false);
        this.showError(err);
      },
    });
  }

  protected openCreateRoot(): void {
    this.openDialog({ mode: 'create' });
  }

  protected addChild(node: CategoryNode): void {
    this.openDialog({ mode: 'create', parentId: node.id, parentName: node.name });
  }

  protected edit(node: CategoryNode): void {
    // ứng viên cha: mọi node trừ chính nó + con cháu (BE chặn cycle) — nhãn thụt lề theo depth.
    const descendants = this.descendantIds(node.id);
    const candidates = this.ordered()
      .filter((n) => !descendants.has(n.id) && n.id !== node.id)
      .map((n) => ({ id: n.id, label: `${'\u00A0\u00A0'.repeat(n.depth)}${n.name}` }));
    this.openDialog({ mode: 'edit', node, candidates });
  }

  protected remove(node: CategoryNode): void {
    this.messages
      .confirm('COMMON_CONFIRM', 'CATEGORY_DELETE_CONFIRM', { name: node.name })
      .pipe(filter(Boolean))
      .subscribe(() => {
        this.categoriesApi.remove(node.id).subscribe({
          next: () => {
            this.messages.success('CATEGORY_DELETED');
            this.reload();
          },
          error: (err: unknown) => this.showError(err),
        });
      });
  }

  private openDialog(data: CategoryDialogData): void {
    const ref = this.dialog.open(CategoryDialog, { data, width: '420px' });
    ref.afterClosed().subscribe((result: CategoryDialogResult | undefined) => {
      if (!result) {
        return;
      }
      if (data.mode === 'create') {
        this.categoriesApi
          .create({ name: result.name, description: result.description, parent_id: data.parentId })
          .subscribe({
            next: () => {
              this.messages.success('CATEGORY_CREATED');
              this.reload();
            },
            error: (err: unknown) => this.showError(err),
          });
        return;
      }
      const req: UpdateCategoryRequest = { name: result.name, description: result.description };
      if (result.moveToRoot) {
        req.move_to_root = true;
      } else if (result.parentId) {
        req.parent_id = result.parentId;
      }
      this.categoriesApi.update(data.node!.id, req).subscribe({
        next: () => {
          this.messages.success('CATEGORY_UPDATED');
          this.reload();
        },
        error: (err: unknown) => this.showError(err),
      });
    });
  }

  /** Tập id con cháu của node (gồm cả node) — chống chọn tổ tiên làm cha. */
  private descendantIds(id: number): Set<number> {
    const set = new Set<number>([id]);
    let grew = true;
    while (grew) {
      grew = false;
      for (const n of this.nodes()) {
        if (n.parent_id && set.has(n.parent_id) && !set.has(n.id)) {
          set.add(n.id);
          grew = true;
        }
      }
    }
    return set;
  }

  private showError(err: unknown): void {
    if (err instanceof ApiRequestError) {
      this.messages.raw('error', err.message);
    } else {
      this.messages.error('LOGIN_ERRORS_GENERIC');
    }
  }
}
