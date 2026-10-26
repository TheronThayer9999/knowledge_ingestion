import { DatePipe, NgTemplateOutlet } from '@angular/common';
import { Component, computed, effect, inject, signal, type OnDestroy } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
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
  articleStage,
  buildFolderTree,
  docTypeKey as docTypeTranslationKey,
  mergeArticleStatuses,
  statusKey,
  subtreeIds,
  type Article,
  type ArticleStage,
  type Category,
  type FolderNode,
} from '../../core/knowledge/knowledge.models';
import { KnowledgeService } from '../../core/knowledge/knowledge.service';
import { MessageService } from '../../shared/feedback/message.service';
import { DocumentDetailDialog, type DocumentDetailData } from './dialogs/document-detail-dialog';
import { DocumentEditDialog, type DocumentEditDialogData } from './dialogs/document-edit-dialog';
import { FolderDialog, type FolderDialogData } from './dialogs/folder-dialog';
import { UploadDialog, type UploadDialogData } from './dialogs/upload-dialog';

type ViewMode = 'list' | 'grid';

/** Nhịp poll trạng thái đầu tiên — câu này chỉ lấy 4 cột/bài nên rất nhẹ */
const STATUS_POLL_MS = 10_000;
/** Trần giãn nhịp: bài kẹt lâu thì 1 phút/lần là đủ, không quét DB liên tục */
const STATUS_POLL_MAX_MS = 60_000;
/**
 * Chặn poll vô hạn khi worker tắt — nhịp chạm trần 60s sau ~1 phút, 60 kỳ
 * ≈ 1 giờ là dừng; thao tác của user (tải lại/upload/sửa) reset lại.
 */
const STATUS_POLL_LIMIT = 60;

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
export class Documents implements OnDestroy {
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
  // `?q=` do ô tìm kiếm trên APP SHELL ghi vào — trang này chỉ đọc, không tự
  // điều hướng. URL là nguồn đúng: không có `q` nghĩa là không lọc theo header.
  private readonly queryParams = toSignal(this.route.queryParamMap, {
    initialValue: this.route.snapshot.queryParamMap,
  });
  /** Worker còn ghi heartbeat không — false thì bài đang chờ sẽ đứng yên */
  protected readonly workerOnline = signal(true);
  protected readonly skeletonRows = [1, 2, 3, 4, 5, 6];

  private loadedOnce = false;
  private statusTimer: ReturnType<typeof setTimeout> | null = null;
  private statusRefreshInFlight = false;
  private statusPolls = 0;
  private statusDelay = STATUS_POLL_MS;

  /** Tab bị ẩn thì ngừng hẹn nhịp — mở lại mới tính tiếp, khỏi quét DB vô ích */
  private readonly onVisibilityChange = (): void => {
    if (!document.hidden) {
      this.scheduleStatusRefresh();
    }
  };

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

  /**
   * Số bài còn trong pipeline (chờ + đang chạy) — chip "đang xử lý" trên
   * toolbar và cảnh báo worker cùng dựa vào đây. Bài lỗi không tính: nó không
   * chờ worker làm gì nữa, báo "đang xử lý" sẽ sai.
   */
  protected readonly activeCount = computed(
    () =>
      this.articles().filter((article) => {
        const stage = articleStage(article);
        return stage === 'pending' || stage === 'processing';
      }).length,
  );

  constructor() {
    this.load();
    document.addEventListener('visibilitychange', this.onVisibilityChange);
    effect(() => {
      const term = this.queryParams()?.get('q') ?? '';
      if (term !== this.search()) {
        this.search.set(term);
      }
    });
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
        this.statusPolls = 0;
        this.statusDelay = STATUS_POLL_MS;
        // Hỏi ngay thay vì chờ hết nhịp đầu để badge + cảnh báo worker đúng
        // từ lúc mở trang; refreshStatuses tự hẹn nhịp kế tiếp.
        this.clearStatusRefresh();
        this.refreshStatuses();
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

  ngOnDestroy(): void {
    document.removeEventListener('visibilitychange', this.onVisibilityChange);
    this.clearStatusRefresh();
  }

  protected stage(article: Article): ArticleStage {
    return articleStage(article);
  }

  protected stageKey(stage: ArticleStage): TranslationKey {
    return statusKey(stage);
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
  protected openFolderDialog(parentId: number | null, autoUpload = false, folder?: Category): void {
    const ref = this.dialog.open<FolderDialog, FolderDialogData, Category>(FolderDialog, {
      data: { categories: this.categories(), parentId, folder },
      maxWidth: '94vw',
      autoFocus: 'first-tabbable',
    });
    ref.afterClosed().subscribe((result) => {
      if (!result) {
        return;
      }
      if (folder) {
        // Sửa: thay tại chỗ + mở nhánh đích để thấy ngay thư mục vừa di chuyển
        this.messages.success('DOC_FOLDER_UPDATED');
        this.categories.update((list) =>
          list.map((item) => (item.id === result.id ? result : item)),
        );
        this.expanded.update((set) => {
          const next = new Set(set).add(result.id);
          if (result.parentId !== null) {
            next.add(result.parentId);
          }
          return next;
        });
        this.load();
        return;
      }
      this.messages.success('DOC_FOLDER_CREATED');
      this.categories.update((list) => [...list, result]);
      this.selected.set(result.id);
      if (result.parentId !== null) {
        this.expanded.update((set) => new Set(set).add(result.parentId as number));
      }
      this.load();
      if (autoUpload) {
        this.openUpload();
      }
    });
  }

  /** Mở dialog sửa thư mục từ menu cây (FolderView không giữ parent_id) */
  protected editFolder(folder: FolderView): void {
    const category = this.categories().find((item) => item.id === folder.id);
    if (category) {
      this.openFolderDialog(category.parentId, false, category);
    }
  }

  /** Sửa tên/thư mục/loại/mô tả — nguồn file/URL là identity, giữ nguyên */
  protected editArticle(article: Article): void {
    const ref = this.dialog.open<DocumentEditDialog, DocumentEditDialogData, Article>(
      DocumentEditDialog,
      {
        data: { article, categories: this.categories() },
        maxWidth: '94vw',
        autoFocus: 'first-tabbable',
      },
    );
    ref.afterClosed().subscribe((updated) => {
      if (!updated) {
        return;
      }
      this.messages.success('DOC_UPDATED');
      // Thay tại chỗ, khỏi tải lại cả danh sách
      this.articles.update((list) => list.map((item) => (item.id === updated.id ? updated : item)));
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

  /**
   * Còn bài chưa xong thì hẹn nhịp kế tiếp để badge tự cập nhật. Nhịp giãn
   * dần 10s → 60s, tab ẩn thì dừng hẳn: badge không đáng để quét DB liên
   * tục, nhất là khi worker kẹt ở compile cả chục phút.
   */
  private scheduleStatusRefresh(): void {
    this.clearStatusRefresh();
    if (document.hidden) {
      return;
    }
    if (this.statusPolls >= STATUS_POLL_LIMIT) {
      return;
    }
    const busy = this.articles().some((article) => articleStage(article) !== 'done');
    if (!busy) {
      this.statusDelay = STATUS_POLL_MS;
      return;
    }
    this.statusPolls += 1;
    this.statusTimer = setTimeout(() => this.refreshStatuses(), this.statusDelay);
    this.statusDelay = Math.min(this.statusDelay * 2, STATUS_POLL_MAX_MS);
  }

  private clearStatusRefresh(): void {
    if (this.statusTimer !== null) {
      clearTimeout(this.statusTimer);
      this.statusTimer = null;
    }
  }

  /**
   * Hỏi đúng 3 cột trạng thái + nhịp sống worker (1 request) rồi gộp vào
   * danh sách đang hiển thị — không tải lại categories + từng danh mục như
   * trước, nên không bật skeleton và UI không nháy.
   */
  private refreshStatuses(): void {
    this.statusTimer = null;
    if (document.hidden || this.statusRefreshInFlight) {
      return;
    }
    this.statusRefreshInFlight = true;
    this.knowledge.getPipelineStatus().subscribe({
      next: (status) => {
        this.statusRefreshInFlight = false;
        this.workerOnline.set(status.workerOnline);
        this.articles.update((articles) => mergeArticleStatuses(articles, status.statuses));
        this.scheduleStatusRefresh();
      },
      error: () => {
        // Lỗi mạng tạm thời: nhịp đã giãn nên cứ để kỳ sau thử lại
        this.statusRefreshInFlight = false;
        this.scheduleStatusRefresh();
      },
    });
  }
}
