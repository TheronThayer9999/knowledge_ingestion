import { Component, computed, inject, signal } from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MAT_DIALOG_DATA, MatDialogModule, MatDialogRef } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { MatSelectModule } from '@angular/material/select';
import { LangService } from '../../../core/i18n/lang.service';
import { buildFolderTree, type Category, type FolderNode } from '../../../core/knowledge/knowledge.models';
import { KnowledgeService } from '../../../core/knowledge/knowledge.service';
import { MessageService } from '../../../shared/feedback/message.service';

export interface FolderDialogData {
  categories: Category[];
  parentId?: number | null;
}

interface FolderOption {
  id: number;
  label: string;
}

/** Giá trị 0 trong select = tạo ở cấp gốc (backend nhận parent_id rỗng) */
const ROOT_OPTION = 0;

/** Tạo thư mục mới, trả về Category vừa tạo để trang chọn luôn thư mục đó */
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

  /** Select cha phẳng theo đúng thứ tự cây, thụt lề theo cấp để dễ nhìn */
  protected readonly options = computed<FolderOption[]>(() =>
    flattenNodes(buildFolderTree(this.data.categories)),
  );

  protected readonly form = inject(FormBuilder).nonNullable.group({
    name: ['', [Validators.required, Validators.maxLength(100)]],
    description: ['', [Validators.maxLength(500)]],
    parentId: [this.data.parentId ?? ROOT_OPTION],
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
    this.saving.set(true);
    this.knowledge
      .createCategory({
        name: value.name.trim(),
        description: value.description.trim() || undefined,
        parentId: value.parentId === ROOT_OPTION ? null : value.parentId,
      })
      .subscribe({
        next: (category) => this.dialogRef.close(category),
        error: (error: unknown) => {
          this.saving.set(false);
          this.messages.errorFromApi(error, 'DOC_FOLDER_CREATE_ERROR');
        },
      });
  }
}

function flattenNodes(nodes: FolderNode[], level = 0, out: FolderOption[] = []): FolderOption[] {
  for (const node of nodes) {
    const indent = '\u00A0\u00A0\u00A0'.repeat(level);
    out.push({ id: node.category.id, label: `${indent}${node.category.name}` });
    flattenNodes(node.children, level + 1, out);
  }
  return out;
}
