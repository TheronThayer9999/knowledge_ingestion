import { DatePipe, NgTemplateOutlet } from '@angular/common';
import { Component, inject, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MAT_DIALOG_DATA, MatDialogModule } from '@angular/material/dialog';
import { MatIconModule } from '@angular/material/icon';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { LangService, type TranslationKey } from '../../../core/i18n/lang.service';
import {
  articleStage,
  docTypeKey as docTypeTranslationKey,
  statusKey as statusTranslationKey,
  type Article,
  type ArticleStage,
  type ArticleTree,
} from '../../../core/knowledge/knowledge.models';
import { KnowledgeService } from '../../../core/knowledge/knowledge.service';

export interface DocumentDetailData {
  article: Article;
  categoryName: string;
}

/**
 * "Xem file": backend không có endpoint tải/preview nên hiển thị metadata
 * + cây mục đã trích từ tài liệu (GET /articles/:id/tree).
 */
@Component({
  selector: 'app-document-detail-dialog',
  imports: [
    DatePipe,
    NgTemplateOutlet,
    MatDialogModule,
    MatButtonModule,
    MatIconModule,
    MatProgressBarModule,
  ],
  templateUrl: './document-detail-dialog.html',
  styleUrl: './document-detail-dialog.scss',
})
export class DocumentDetailDialog {
  protected readonly data = inject<DocumentDetailData>(MAT_DIALOG_DATA);
  protected readonly lang = inject(LangService);
  private readonly knowledge = inject(KnowledgeService);

  protected readonly tree = signal<ArticleTree | null>(null);
  protected readonly loading = signal(true);
  protected readonly failed = signal(false);

  constructor() {
    this.loadTree();
  }

  protected docTypeKey(docType: string): TranslationKey {
    return docTypeTranslationKey(docType);
  }

  protected statusKey(status: string): TranslationKey {
    return statusTranslationKey(status);
  }

  /** Trạng thái gộp extract → index → compile của bài đang mở */
  protected stage(): ArticleStage {
    return articleStage(this.data.article);
  }

  protected loadTree(): void {
    this.loading.set(true);
    this.failed.set(false);
    this.knowledge.getArticleTree(this.data.article.id).subscribe({
      next: (tree) => {
        this.tree.set(tree);
        this.loading.set(false);
      },
      error: () => {
        this.loading.set(false);
        this.failed.set(true);
      },
    });
  }
}
