import { Component, computed, inject, signal } from '@angular/core';
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
import {
  KnowledgeService,
  type UploadFileOutcome,
  type UploadProgress,
  type UploadStage,
} from '../../../core/knowledge/knowledge.service';
import { MessageService } from '../../../shared/feedback/message.service';

export interface UploadDialogData {
  categories: Category[];
  defaultCategoryId?: number | null;
}

const MAX_FILE_BYTES = 500 * 1024 * 1024;

/** Trần số file mỗi lượt — khớp MAX_UPLOADS_PER_REQUEST của backend */
const MAX_FILES = 20;

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

type RowStatus = 'pending' | 'uploading' | 'creating' | 'done' | 'error';

/** Một dòng trong danh sách chờ upload — tên sửa được trước khi bấm Upload */
interface UploadRow {
  id: number;
  file: File;
  name: string;
  status: RowStatus;
  error: string | null;
}

const ROW_STATUS_LABELS: Record<RowStatus, TranslationKey> = {
  pending: 'DOC_UPLOAD_ROW_PENDING',
  uploading: 'DOC_UPLOAD_ROW_UPLOADING',
  creating: 'DOC_UPLOAD_ROW_CREATING',
  done: 'DOC_UPLOAD_ROW_DONE',
  error: 'DOC_UPLOAD_ROW_ERROR',
};

const ROW_STATUS_ICONS: Record<RowStatus, string> = {
  pending: 'draft',
  uploading: 'cloud_upload',
  creating: 'autorenew',
  done: 'check_circle',
  error: 'error',
};

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

/**
 * Upload nhiều file một lượt: chọn/kéo thả cả loạt, sửa tên từng dòng, rồi
 * presign cả lô (1 request) → PUT kho → tạo article lần lượt từng file.
 */
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
  protected readonly maxFiles = MAX_FILES;
  private readonly dialogRef = inject<MatDialogRef<UploadDialog, Article[]>>(MatDialogRef);
  private readonly knowledge = inject(KnowledgeService);
  private readonly messages = inject(MessageService);

  protected readonly rows = signal<UploadRow[]>([]);
  protected readonly fileError = signal<TranslationKey | null>(null);
  protected readonly dragging = signal(false);
  protected readonly stage = signal<UploadStage | null>(null);
  protected readonly submitting = signal(false);
  /** Đã bấm Upload ít nhất 1 lần — để tô đỏ dòng còn tên trống */
  protected readonly attempted = signal(false);
  protected readonly hasFailure = computed(() => this.rows().some((row) => row.status === 'error'));

  protected readonly docTypes: DocTypeOption[] = DOC_TYPES.map((value) => ({
    value,
    key: DOC_TYPE_KEYS[value],
  }));

  protected readonly form = inject(FormBuilder).nonNullable.group({
    docType: ['general' as DocType],
    categoryId: [
      this.data.defaultCategoryId ?? this.data.categories[0]?.id ?? 0,
      [Validators.min(1)],
    ],
    description: ['', [Validators.maxLength(1000)]],
  });

  /** Article đã tạo qua các lần bấm (giữ lại để trả về khi cả lô xong) */
  private readonly created: Article[] = [];
  private nextRowId = 0;

  protected onPick(event: Event): void {
    const input = event.target as HTMLInputElement;
    const picked = Array.from(input.files ?? []);
    input.value = ''; // cho phép chọn lại đúng file vừa bỏ khỏi danh sách
    this.addFiles(picked);
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
    this.addFiles(Array.from(event.dataTransfer?.files ?? []));
  }

  protected renameRow(index: number, event: Event): void {
    const value = (event.target as HTMLInputElement).value;
    const row = this.rows()[index];
    if (row) {
      this.patchRow(row.id, { name: value });
    }
  }

  protected removeRow(index: number): void {
    if (this.submitting()) {
      return;
    }
    this.rows.update((list) => list.filter((_, i) => i !== index));
    this.fileError.set(null);
  }

  protected statusKey(status: RowStatus): TranslationKey {
    return ROW_STATUS_LABELS[status];
  }

  protected statusIcon(status: RowStatus): string {
    return ROW_STATUS_ICONS[status];
  }

  protected isNameBlank(row: UploadRow): boolean {
    return this.attempted() && row.name.trim().length === 0;
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
    const queue = this.rows().filter((row) => row.status !== 'done');
    if (queue.length === 0) {
      this.fileError.set('DOC_UPLOAD_REQUIRED_FILE');
      return;
    }
    this.attempted.set(true);
    if (queue.some((row) => !row.name.trim())) {
      this.fileError.set('DOC_UPLOAD_REQUIRED_NAME');
      return;
    }
    if (this.form.invalid) {
      this.form.markAllAsTouched();
      return;
    }
    const value = this.form.getRawValue();
    this.fileError.set(null);
    this.submitting.set(true);
    this.stage.set(null);
    for (const row of queue) {
      this.patchRow(row.id, { status: 'pending', error: null });
    }
    this.knowledge
      .uploadDocuments(
        queue.map((row) => ({ file: row.file, name: row.name.trim() })),
        {
          categoryId: value.categoryId,
          docType: value.docType,
          description: value.description.trim() || undefined,
        },
        (progress) => this.applyProgress(queue, progress),
      )
      .subscribe({
        next: (outcomes) => this.finish(queue, outcomes),
        error: (error: unknown) => this.failBatch(queue, error),
      });
  }

  /** Đẩy trạng thái tiến trình về đúng dòng; presign là bước của cả lô */
  private applyProgress(queue: UploadRow[], progress: UploadProgress): void {
    this.advanceStage(progress.stage);
    if (progress.index === null) {
      return;
    }
    const row = queue[progress.index];
    if (row) {
      this.patchRow(row.id, {
        status: progress.stage === 'create' ? 'creating' : 'uploading',
      });
    }
  }

  /** Dải 3 bước chỉ tiến, không lùi khi file sau quay lại bước upload */
  private advanceStage(stage: UploadStage): void {
    const current = this.stage();
    if (!current || STAGE_ORDER.indexOf(stage) > STAGE_ORDER.indexOf(current)) {
      this.stage.set(stage);
    }
  }

  private finish(queue: UploadRow[], outcomes: UploadFileOutcome[]): void {
    let failed = 0;
    outcomes.forEach((outcome, index) => {
      const row = queue[index];
      if (!row) {
        return;
      }
      if (outcome.article) {
        this.created.push(outcome.article);
        this.patchRow(row.id, { status: 'done', error: null });
        return;
      }
      failed += 1;
      this.patchRow(row.id, {
        status: 'error',
        error: errorText(outcome.error, this.lang.t('DOC_UPLOAD_ERROR')),
      });
    });
    this.submitting.set(false);
    if (failed > 0) {
      // Giữ dialog mở để bấm Upload lại — lần sau chỉ đẩy các dòng chưa done
      this.fileError.set('DOC_UPLOAD_PARTIAL');
      return;
    }
    this.stage.set(null);
    this.dialogRef.close(this.created);
  }

  /** Presign hỏng là cả lô hỏng (backend validate trọn lô trước khi ký) */
  private failBatch(queue: UploadRow[], error: unknown): void {
    const text = errorText(error, this.lang.t('DOC_UPLOAD_ERROR'));
    for (const row of queue) {
      this.patchRow(row.id, { status: 'error', error: text });
    }
    this.submitting.set(false);
    this.stage.set(null);
    this.fileError.set('DOC_UPLOAD_ERROR');
    this.messages.errorFromApi(error, 'DOC_UPLOAD_ERROR');
  }

  private patchRow(id: number, patch: Partial<UploadRow>): void {
    this.rows.update((list) => list.map((row) => (row.id === id ? { ...row, ...patch } : row)));
  }

  /** Nhận cả loạt file vừa chọn/kéo thả: giữ file hợp lệ, báo lỗi file bị loại */
  private addFiles(picked: File[]): void {
    if (picked.length === 0) {
      return;
    }
    const room = Math.max(MAX_FILES - this.rows().length, 0);
    const accepted: UploadRow[] = [];
    let error: TranslationKey | null = null;
    for (const file of picked) {
      const invalid = validateFile(file);
      if (invalid) {
        error ??= invalid;
        continue;
      }
      if (accepted.length >= room) {
        error = 'DOC_UPLOAD_MAX_FILES';
        continue;
      }
      accepted.push({
        id: this.nextRowId++,
        file,
        // Giữ NGUYÊN VĂN tên file (kể cả đuôi): đó là tên user đặt, và BE còn
        // dựa vào đuôi trong tên để đoán template chunking (.xlsx/.csv -> table).
        name: file.name,
        status: 'pending',
        error: null,
      });
    }
    this.fileError.set(error);
    if (accepted.length > 0) {
      this.rows.update((list) => [...list, ...accepted]);
    }
  }
}

/** Kiểm tra đuôi + dung lượng ngay trên client để báo lỗi sớm, không tốn 1 vòng API */
function validateFile(file: File): TranslationKey | null {
  const ext = file.name.split('.').pop()?.toLowerCase() ?? '';
  if (!ACCEPTED_EXTS.includes(ext)) {
    return 'DOC_UPLOAD_INVALID_TYPE';
  }
  if (file.size > MAX_FILE_BYTES) {
    return 'DOC_UPLOAD_TOO_LARGE';
  }
  return null;
}

/** Message lỗi từ backend đã là tiếng Việt và được phép lộ ra client */
function errorText(error: unknown, fallback: string): string {
  const message = (error as { message?: unknown } | null)?.message;
  return typeof message === 'string' && message.trim().length > 0 ? message : fallback;
}
