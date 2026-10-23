import { Component, inject, signal } from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MAT_DIALOG_DATA, MatDialogModule, MatDialogRef } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { MatSelectModule } from '@angular/material/select';
import { LangService } from '../../../core/i18n/lang.service';
import {
  DOC_TYPES,
  docTypeKey,
  flattenCategories,
  type Article,
  type Category,
  type DocType,
  type UpdateArticlePayload,
} from '../../../core/knowledge/knowledge.models';
import { KnowledgeService } from '../../../core/knowledge/knowledge.service';
import { MessageService } from '../../../shared/feedback/message.service';

export interface DocumentEditDialogData {
  article: Article;
  categories: Category[];
}

/**
 * Sửa thông tin tài liệu (tên / thư mục / loại / mô tả). Nguồn (file trong kho
 * hoặc URL) là identity của article nên không sửa được — muốn thay nội dung
 * phải xoá rồi upload lại; hint trong dialog nói rõ điều này.
 */
@Component({
  selector: 'app-document-edit-dialog',
  imports: [
    ReactiveFormsModule,
    MatDialogModule,
    MatFormFieldModule,
    MatInputModule,
    MatSelectModule,
    MatButtonModule,
    MatIconModule,
    MatProgressBarModule,
  ],
  templateUrl: './document-edit-dialog.html',
  styleUrl: './document-edit-dialog.scss',
})
export class DocumentEditDialog {
  protected readonly data = inject<DocumentEditDialogData>(MAT_DIALOG_DATA);
  protected readonly lang = inject(LangService);
  private readonly dialogRef = inject<MatDialogRef<DocumentEditDialog, Article>>(MatDialogRef);
  private readonly knowledge = inject(KnowledgeService);
  private readonly messages = inject(MessageService);

  protected readonly saving = signal(false);

  protected readonly docTypes = DOC_TYPES.map((value) => ({ value, key: docTypeKey(value) }));

  /** Chọn thư mục đích — phẳng theo cây để thấy được quan hệ cha/con */
  protected readonly options = flattenCategories(this.data.categories);

  protected readonly form = inject(FormBuilder).nonNullable.group({
    name: [this.data.article.name, [Validators.required, Validators.maxLength(255)]],
    categoryId: [this.data.article.categoryId, [Validators.min(1)]],
    docType: [(this.data.article.docType || 'general') as DocType],
    description: [this.data.article.description, [Validators.maxLength(1000)]],
  });

  protected submit(): void {
    if (this.saving()) {
      return;
    }
    if (this.form.invalid) {
      this.form.markAllAsTouched();
      return;
    }
    const article = this.data.article;
    const value = this.form.getRawValue();
    const name = value.name.trim();
    const description = value.description.trim();

    // Chỉ gửi field thật sự đổi — PUT của backend là partial update
    const payload: UpdateArticlePayload = {};
    if (name !== article.name) {
      payload.name = name;
    }
    if (value.categoryId !== article.categoryId) {
      payload.categoryId = value.categoryId;
    }
    if (value.docType !== article.docType) {
      payload.docType = value.docType;
    }
    if (description !== article.description) {
      payload.description = description;
    }

    // Không đổi gì thì đóng luôn, khỏi tốn 1 vòng API
    if (Object.keys(payload).length === 0) {
      this.dialogRef.close(article);
      return;
    }

    this.saving.set(true);
    this.knowledge.updateArticle(article.id, payload).subscribe({
      next: (updated) => this.dialogRef.close(updated),
      error: (error: unknown) => {
        this.saving.set(false);
        this.messages.errorFromApi(error, 'DOC_UPDATE_ERROR');
      },
    });
  }
}
