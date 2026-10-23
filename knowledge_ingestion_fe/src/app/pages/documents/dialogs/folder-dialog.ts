import { Component, computed, inject, signal } from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MAT_DIALOG_DATA, MatDialogModule, MatDialogRef } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { MatSelectModule } from '@angular/material/select';
import { LangService } from '../../../core/i18n/lang.service';
import { Observable } from 'rxjs';
import {
  flattenCategories,
  subtreeIds,
  type Category,
  type CategoryOption,
  type UpdateCategoryPayload,
} from '../../../core/knowledge/knowledge.models';
import { KnowledgeService } from '../../../core/knowledge/knowledge.service';
import { MessageService } from '../../../shared/feedback/message.service';

export interface FolderDialogData {
  categories: Category[];
  parentId?: number | null;
  /** Có `folder` = chế độ sửa (đổi tên / mô tả / di chuyển), ngược lại là tạo mới */
  folder?: Category;
}

/** Giá trị 0 trong select = tạo ở cấp gốc (backend nhận parent_id rỗng) */
const ROOT_OPTION = 0;

/**
 * Tạo hoặc sửa thư mục. Chế độ sửa không cho chọn chính nó/thư mục con làm
 * cha (backend cũng chặn — lọc trước để người dùng không chọn được rồi lỗi).
 */
@Component({
  selector: 'app-folder-dialog',
  imports: [
    ReactiveFormsModule,
    MatDialogModule,
    MatFormFieldModule,
    MatInputModule,
    MatSelectModule,
    MatButtonModule,
    MatProgressBarModule,
  ],
  templateUrl: './folder-dialog.html',
  styleUrl: './folder-dialog.scss',
})
export class FolderDialog {
  protected readonly data = inject<FolderDialogData>(MAT_DIALOG_DATA);
  protected readonly lang = inject(LangService);
  private readonly dialogRef = inject<MatDialogRef<FolderDialog, Category>>(MatDialogRef);
  private readonly knowledge = inject(KnowledgeService);
  private readonly messages = inject(MessageService);

  protected readonly saving = signal(false);
  protected readonly editing = this.data.folder ?? null;

  /** Select cha phẳng theo đúng thứ tự cây, thụt lề theo cấp để dễ nhìn */
  protected readonly options = computed<CategoryOption[]>(() => {
    const blocked = this.editing
      ? subtreeIds(this.data.categories, this.editing.id)
      : undefined;
    return flattenCategories(this.data.categories, blocked);
  });

  protected readonly form = inject(FormBuilder).nonNullable.group({
    name: [
      this.editing?.name ?? '',
      [Validators.required, Validators.maxLength(100)],
    ],
    description: [this.editing?.description ?? '', [Validators.maxLength(500)]],
    parentId: [this.editing?.parentId ?? this.data.parentId ?? ROOT_OPTION],
  });

  protected submit(): void {
    if (this.saving()) {
      return;
    }
    if (this.form.invalid) {
      this.form.markAllAsTouched();
      return;
    }
    const value = this.form.getRawValue();
    const name = value.name.trim();
    const description = value.description.trim();
    const parentId = value.parentId === ROOT_OPTION ? null : value.parentId;
    this.saving.set(true);

    const folder = this.editing;
    if (folder) {
      const payload: UpdateCategoryPayload = { name, description };
      if (parentId !== folder.parentId) {
        if (parentId === null) {
          payload.moveToRoot = true;
        } else {
          payload.parentId = parentId;
        }
      }
      this.save(this.knowledge.updateCategory(folder.id, payload));
      return;
    }

    this.save(
      this.knowledge.createCategory({
        name,
        description: description || undefined,
        parentId,
      }),
    );
  }

  private save(request$: Observable<Category>): void {
    request$.subscribe({
      next: (category) => this.dialogRef.close(category),
      error: (error: unknown) => {
        this.saving.set(false);
        this.messages.errorFromApi(
          error,
          this.editing ? 'DOC_FOLDER_UPDATE_ERROR' : 'DOC_FOLDER_CREATE_ERROR',
        );
      },
    });
  }
}
