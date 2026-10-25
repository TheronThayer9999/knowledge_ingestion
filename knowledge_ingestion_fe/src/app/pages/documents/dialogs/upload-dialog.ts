import { Component, inject, signal } from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MAT_DIALOG_DATA, MatDialogModule, MatDialogRef } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { MatSelectModule } from '@angular/material/select';
import { LangService, type TranslationKey } from '../../../core/i18n/lang.service';
import {
  DOC_TYPES,
  type Article,
  type Category,
  type DocType,
} from '../../../core/knowledge/knowledge.models';
import { KnowledgeService, type UploadStage } from '../../../core/knowledge/knowledge.service';
import { MessageService } from '../../../shared/feedback/message.service';

export interface UploadDialogData {
  categories: Category[];
  defaultCategoryId?: number | null;
}

const MAX_FILE_BYTES = 500 * 1024 * 1024;

/** Whitelist đuôi file đúng như backend (file_service.allowedExts) */
const ACCEPTED_EXTS = [
  'pdf',
  'docx',
  'xlsx',
  'pptx',
  'txt',
  'md',
  'csv',
  'json',
  'jpg',
  'jpeg',
  'png',
  'webp',
];

const STAGE_ORDER: UploadStage[] = ['presign', 'upload', 'create'];

const STAGE_LABELS: { stage: UploadStage; key: TranslationKey }[] = [
  { stage: 'presign', key: 'DOC_UPLOAD_STAGE_PRESIGN' },
  { stage: 'upload', key: 'DOC_UPLOAD_STAGE_UPLOAD' },
  { stage: 'create', key: 'DOC_UPLOAD_STAGE_CREATE' },
];

interface DocTypeOption {
  value: DocType;
  key: TranslationKey;
}

const DOC_TYPE_KEYS: Record<DocType, TranslationKey> = {
  general: 'DOC_TYPE_GENERAL',
  laws: 'DOC_TYPE_LAWS',
  table: 'DOC_TYPE_TABLE',
  paper: 'DOC_TYPE_PAPER',
  book: 'DOC_TYPE_BOOK',
  qa: 'DOC_TYPE_QA',
  manual: 'DOC_TYPE_MANUAL',
};

/** Upload 3 bước (presign → PUT kho → tạo article) kèm tiến trình từng bước */
@Component({
  selector: 'app-upload-dialog',
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
  templateUrl: './upload-dialog.html',
  styleUrl: './upload-dialog.scss',
})
export class UploadDialog {
  protected readonly data = inject<UploadDialogData>(MAT_DIALOG_DATA);
  protected readonly lang = inject(LangService);
  protected readonly accept = ACCEPTED_EXTS.map((ext) => `.${ext}`).join(',');
  protected readonly stages = STAGE_LABELS;
  private readonly dialogRef = inject<MatDialogRef<UploadDialog, Article>>(MatDialogRef);
  private readonly knowledge = inject(KnowledgeService);
  private readonly messages = inject(MessageService);

  protected readonly file = signal<File | null>(null);
  protected readonly fileError = signal<TranslationKey | null>(null);
  protected readonly dragging = signal(false);
  protected readonly stage = signal<UploadStage | null>(null);
  protected readonly submitting = signal(false);

  protected readonly docTypes: DocTypeOption[] = DOC_TYPES.map((value) => ({
    value,
    key: DOC_TYPE_KEYS[value],
  }));

  protected readonly form = inject(FormBuilder).nonNullable.group({
    name: ['', [Validators.required, Validators.maxLength(255)]],
    docType: ['general' as DocType],
    categoryId: [
      this.data.defaultCategoryId ?? this.data.categories[0]?.id ?? 0,
      [Validators.min(1)],
    ],
    description: ['', [Validators.maxLength(1000)]],
  });

  protected onPick(event: Event): void {
    const input = event.target as HTMLInputElement;
    const picked = input.files?.[0];
    input.value = '';
    if (picked) {
      this.applyFile(picked);
    }
  }

  protected onDragOver(event: DragEvent): void {
    event.preventDefault();
    if (!this.submitting()) {
      this.dragging.set(true);
    }
  }

  protected onDragLeave(event: DragEvent): void {
    event.preventDefault();
    this.dragging.set(false);
  }

  protected onDrop(event: DragEvent): void {
    event.preventDefault();
    this.dragging.set(false);
    if (this.submitting()) {
      return;
    }
    const dropped = event.dataTransfer?.files[0];
    if (dropped) {
      this.applyFile(dropped);
    }
  }

  protected stageState(stage: UploadStage): 'done' | 'active' | 'pending' {
    const current = this.stage();
    if (!current) {
      return 'pending';
    }
    const delta = STAGE_ORDER.indexOf(current) - STAGE_ORDER.indexOf(stage);
    if (delta > 0) {
      return 'done';
    }
    return delta === 0 ? 'active' : 'pending';
  }

  protected formatSize(bytes: number): string {
    if (bytes < 1024) {
      return `${bytes} B`;
    }
    const kb = bytes / 1024;
    if (kb < 1024) {
      return `${kb.toFixed(0)} KB`;
    }
    return `${(kb / 1024).toFixed(1)} MB`;
  }

  protected submit(): void {
    if (this.submitting()) {
      return;
    }
    const picked = this.file();
    if (!picked) {
      this.fileError.set('DOC_UPLOAD_REQUIRED_FILE');
      return;
    }
    if (this.form.invalid) {
      this.form.markAllAsTouched();
      return;
    }
    const value = this.form.getRawValue();
    this.fileError.set(null);
    this.submitting.set(true);
    this.knowledge
      .uploadDocument(
        picked,
        {
          name: value.name.trim(),
          categoryId: value.categoryId,
          docType: value.docType,
          description: value.description.trim() || undefined,
        },
        (stage) => this.stage.set(stage),
      )
      .subscribe({
        next: (article) => this.dialogRef.close(article),
        error: (error: unknown) => {
          this.submitting.set(false);
          this.stage.set(null);
          this.messages.errorFromApi(error, 'DOC_UPLOAD_ERROR');
        },
      });
  }

  /** Kiểm tra đuôi + dung lượng ngay trên client để báo lỗi sớm, không tốn 1 vòng API */
  private applyFile(picked: File): void {
    const ext = picked.name.split('.').pop()?.toLowerCase() ?? '';
    if (!ACCEPTED_EXTS.includes(ext)) {
      this.fileError.set('DOC_UPLOAD_INVALID_TYPE');
      return;
    }
    if (picked.size > MAX_FILE_BYTES) {
      this.fileError.set('DOC_UPLOAD_TOO_LARGE');
      return;
    }
    this.fileError.set(null);
    this.file.set(picked);
    const nameControl = this.form.controls.name;
    if (!nameControl.value.trim()) {
      nameControl.setValue(picked.name.replace(/\.[^.]+$/, ''));
    }
  }
}
