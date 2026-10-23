import { DatePipe, NgTemplateOutlet } from '@angular/common';
import { Component, computed, inject, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatButtonToggleModule } from '@angular/material/button-toggle';
import { MatCardModule } from '@angular/material/card';
import { MatDialog } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatMenuModule } from '@angular/material/menu';
import { MatTooltipModule } from '@angular/material/tooltip';
import { ActivatedRoute, Router } from '@angular/router';
import { forkJoin } from 'rxjs';
import { LangService, type TranslationKey } from '../../core/i18n/lang.service';
import {
  buildFolderTree,
  docTypeKey as docTypeTranslationKey,
  subtreeIds,
  type Article,
  type Category,
  type FolderNode,
} from '../../core/knowledge/knowledge.models';
import { KnowledgeService } from '../../core/knowledge/knowledge.service';
import { MessageService } from '../../shared/feedback/message.service';
import { DocumentDetailDialog, type DocumentDetailData } from './dialogs/document-detail-dialog';
import { FolderDialog, type FolderDialogData } from './dialogs/folder-dialog';
import { UploadDialog, type UploadDialogData } from './dialogs/upload-dialog';

type ViewMode = 'list' | 'grid';

/** Node thư mục kèm số tài liệu của cả nhánh để hiện cạnh tên */
interface FolderView {
  id: number;
  name: string;
  description: string;
  children: FolderView[];
  count: number;
}

/**
 * Quản trị tài liệu: cây thư mục bên trái, danh sách/lưới tài liệu bên phải.
 * Backend chỉ list theo từng danh mục nên tải tất cả một lần rồi lọc phía
 * client (tìm kiếm + lọc cả nhánh thư mục đang chọn).
 */
@Component({
  selector: 'app-documents',
  imports: [
    DatePipe,
    NgTemplateOutlet,
    MatCardModule,
    MatIconModule,
    MatButtonModule,
    MatButtonToggleModule,
    MatFormFieldModule,
    MatInputModule,
    MatMenuModule,
    MatTooltipModule,
  ],
  templateUrl: './documents.html',
  styleUrl: './documents.scss',
})
export class Documents {
  protected readonly lang = inject(LangService);
  private readonly knowledge = inject(KnowledgeService);
  private readonly messages = inject(MessageService);
  private readonly dialog = inject(MatDialog);
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);

  protected readonly categories = signal<Category[]>([]);
  protected readonly articles = signal<Article[]>([]);
  protected readonly selected = signal<number | null>(null);
  protected readonly expanded = signal<ReadonlySet<number>>(new Set());
  protected readonly search = signal('');
  protected readonly view = signal<ViewMode>('list');
  protected readonly loading = signal(true);
  protected readonly failed = signal(false);
  protected readonly skeletonRows = [1, 2, 3, 4, 5, 6];

  private loadedOnce = false;

  /** Cây thư mục + đếm tài liệu theo cả nhánh */
  protected readonly folderViews = computed<FolderView[]>(() => {
    const counts = new Map<number, number>();
    for (const article of this.articles()) {
      counts.set(article.categoryId, (counts.get(article.categoryId) ?? 0) + 1);
    }
    const decorate = (node: FolderNode): FolderView => {
      const children = node.children.map(decorate);
      const own = counts.get(node.category.id) ?? 0;
      return {
        id: node.category.id,
        name: node.category.name,
        description: node.category.description,
        children,
        count: own + children.reduce((sum, child) => sum + child.count, 0),
      };
    };
    return buildFolderTree(this.categories()).map(decorate);
  });

  /** Tài liệu đang hiển thị: lọc theo nhánh thư mục đang chọn + từ khóa */
  protected readonly visible = computed<Article[]>(() => {
    const term = this.search().trim().toLowerCase();
    const folder = this.selected();
    const allowed = folder === null ? null : subtreeIds(this.categories(), folder);
    return this.articles()
      .filter((article) => allowed === null || allowed.has(article.categoryId))
      .filter((article) => term === '' || article.name.toLowerCase().includes(term))
      .sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt));
  });

  constructor() {
    this.load();
  }

  protected load(): void {
    this.loading.set(true);
    this.failed.set(false);
    forkJoin({
      categories: this.knowledge.listCategories(),
      articles: this.knowledge.listAllArticles(),
    }).subscribe({
      next: ({ categories, articles }) => {
        this.categories.set(categories);
        this.articles.set(articles);
        this.loading.set(false);
        if (!this.loadedOnce) {
          this.loadedOnce = true;
          this.expanded.set(
            new Set(categories.filter((item) => item.parentId === null).map((item) => item.id)),
          );
          this.handleQuickAction();
        }
      },
      error: (error: unknown) => {
        this.loading.set(false);
        this.failed.set(true);
        this.messages.errorFromApi(error, 'DOC_LOAD_ERROR');
      },
    });
  }

  protected select(folderId: number | null): void {
    this.selected.set(folderId);
  }

  protected isExpanded(folderId: number): boolean {
    return this.expanded().has(folderId);
  }

  protected toggleExpand(folderId: number): void {
    this.expanded.update((set) => {
      const next = new Set(set);
      if (next.has(folderId)) {
        next.delete(folderId);
      } else {
        next.add(folderId);
      }
      return next;
    });
  }

  protected onSearch(event: Event): void {
    this.search.set((event.target as HTMLInputElement).value);
  }

  protected clearSearch(): void {
    this.search.set('');
  }

  protected setView(mode: ViewMode): void {
    this.view.set(mode);
  }

  protected categoryName(categoryId: number): string {
    return this.categories().find((category) => category.id === categoryId)?.name ?? '';
  }

  protected docTypeKey(docType: string): TranslationKey {
    return docTypeTranslationKey(docType);
  }

  protected openUpload(): void {
    if (this.categories().length === 0) {
      this.messages.warning('DOC_UPLOAD_NO_FOLDER');
      this.openFolderDialog(null, true);
      return;
    }
    const ref = this.dialog.open<UploadDialog, UploadDialogData, Article>(UploadDialog, {
      data: { categories: this.categories(), defaultCategoryId: this.selected() },
      maxWidth: '94vw',
      autoFocus: 'first-tabbable',
    });
    ref.afterClosed().subscribe((article) => {
      if (!article) {
        return;
      }
      this.messages.success('DOC_UPLOAD_SUCCESS');
      this.selected.set(article.categoryId);
      this.load();
    });
  }

  /** `autoUpload`: tạo thư mục đầu tiên xong mở luôn hộp thoại upload */
  protected openFolderDialog(parentId: number | null, autoUpload = false): void {
    const ref = this.dialog.open<FolderDialog, FolderDialogData, Category>(FolderDialog, {
      data: { categories: this.categories(), parentId },
      maxWidth: '94vw',
      autoFocus: 'first-tabbable',
    });
    ref.afterClosed().subscribe((created) => {
      if (!created) {
        return;
      }
      this.messages.success('DOC_FOLDER_CREATED');
      this.categories.update((list) => [...list, created]);
      this.selected.set(created.id);
      if (created.parentId !== null) {
        this.expanded.update((set) => new Set(set).add(created.parentId as number));
      }
      this.load();
      if (autoUpload) {
        this.openUpload();
      }
    });
  }

  protected openDetail(article: Article): void {
    this.dialog.open<DocumentDetailDialog, DocumentDetailData>(DocumentDetailDialog, {
      data: { article, categoryName: this.categoryName(article.categoryId) },
      maxWidth: '94vw',
      autoFocus: 'dialog',
    });
  }

  protected async deleteArticle(article: Article): Promise<void> {
    const confirmed = await this.messages.confirm(
      'DOC_DELETE_CONFIRM_TITLE',
      'DOC_DELETE_CONFIRM_MESSAGE',
    );
    if (!confirmed) {
      return;
    }
    this.knowledge.deleteArticle(article.id).subscribe({
      next: () => {
        this.messages.success('DOC_DELETED');
        this.load();
      },
      error: (error: unknown) => this.messages.errorFromApi(error, 'DOC_DELETE_ERROR'),
    });
  }

  protected async deleteFolder(folder: FolderView): Promise<void> {
    const confirmed = await this.messages.confirm(
      'DOC_FOLDER_DELETE_CONFIRM_TITLE',
      'DOC_FOLDER_DELETE_CONFIRM_MESSAGE',
    );
    if (!confirmed) {
      return;
    }
    const removed = subtreeIds(this.categories(), folder.id);
    this.knowledge.deleteCategory(folder.id).subscribe({
      next: () => {
        this.messages.success('DOC_FOLDER_DELETED');
        const current = this.selected();
        if (current !== null && removed.has(current)) {
          this.selected.set(null);
        }
        this.expanded.update((set) => new Set([...set].filter((id) => !removed.has(id))));
        this.load();
      },
      error: (error: unknown) => this.messages.errorFromApi(error, 'DOC_FOLDER_DELETE_ERROR'),
    });
  }

  /** Mở nhanh upload/tạo thư mục khi được điều hướng từ trang Tổng quan */
  private handleQuickAction(): void {
    const query = this.route.snapshot.queryParamMap;
    const wantsUpload = query.has('upload');
    const wantsFolder = query.has('newFolder');
    if (wantsUpload) {
      this.openUpload();
    } else if (wantsFolder) {
      this.openFolderDialog(null);
    }
    if (wantsUpload || wantsFolder) {
      void this.router.navigate([], {
        relativeTo: this.route,
        queryParams: {},
        replaceUrl: true,
      });
    }
  }
}
