import { Component, inject, signal } from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MAT_DIALOG_DATA, MatDialogModule, MatDialogRef } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatProgressSpinnerModule } from '@angular/material/progress-spinner';
import { MatSelectModule } from '@angular/material/select';
import { ApiRequestError } from '../../core/api/api.helpers';
import { Article } from '../../core/api/api.models';
import { ArticlesService } from '../../core/api/articles.service';
import { UploadsService } from '../../core/api/uploads.service';
import { MessageService } from '../../shared/feedback/message.service';
import { LangService } from '../../shared/i18n/lang.service';
import { TranslationKey } from '../../shared/i18n/lang.model';

/** doc_type — khớp oneof binding của dtos.CreateArticleRequest bên Go. */
const DOC_TYPES = ['general', 'laws', 'table', 'paper', 'book', 'qa', 'manual'] as const;

/** doc_type → key i18n nhãn hiển thị (key tĩnh, không ghép chuỗi runtime). */
const DOC_TYPE_KEYS: Record<(typeof DOC_TYPES)[number], TranslationKey> = {
  general: 'ARTICLES_DOCTYPE_GENERAL',
  laws: 'ARTICLES_DOCTYPE_LAWS',
  table: 'ARTICLES_DOCTYPE_TABLE',
  paper: 'ARTICLES_DOCTYPE_PAPER',
  book: 'ARTICLES_DOCTYPE_BOOK',
  qa: 'ARTICLES_DOCTYPE_QA',
  manual: 'ARTICLES_DOCTYPE_MANUAL',
};

interface CreateDialogData {
  candidates: { id: number; label: string }[];
}

/**
 * Dialog tạo tài liệu — đúng 1 nguồn:
 *  - URL: link web, content_type mặc định text/html.
 *  - File: presign → PUT thẳng SeaweedFS → gửi storage_key (BE HeadObject verify).
 */
@Component({
  selector: 'app-article-create-dialog',
  imports: [
    MatDialogModule,
    ReactiveFormsModule,
    MatFormFieldModule,
    MatInputModule,
    MatSelectModule,
    MatButtonModule,
    MatIconModule,
    MatProgressSpinnerModule,
  ],
  template: `
    <h2 mat-dialog-title>{{ lang.t('ARTICLES_CREATE_TITLE') }}</h2>
    <form [formGroup]="form" (ngSubmit)="save()">
      <mat-dialog-content>
        <div class="article-create__source-toggle">
          <button
            mat-stroked-button
            type="button"
            [color]="source() === 'url' ? 'primary' : undefined"
            (click)="setSource('url')"
          >
            <mat-icon>link</mat-icon>
            {{ lang.t('ARTICLES_SOURCE_URL') }}
          </button>
          <button
            mat-stroked-button
            type="button"
            [color]="source() === 'file' ? 'primary' : undefined"
            (click)="setSource('file')"
          >
            <mat-icon>upload_file</mat-icon>
            {{ lang.t('ARTICLES_SOURCE_FILE') }}
          </button>
        </div>

        @if (source() === 'url') {
          <mat-form-field appearance="outline">
            <mat-label>{{ lang.t('ARTICLES_FIELD_URL') }}</mat-label>
            <input matInput formControlName="url" placeholder="https://example.com/docs" />
            @if (form.controls.url.touched && form.controls.url.invalid) {
              <mat-error>{{ lang.t('ARTICLES_URL_ERROR') }}</mat-error>
            }
          </mat-form-field>
        } @else {
          <div class="article-create__file">
            <input type="file" #fileInput hidden (change)="pickFile(fileInput)" />
            <button mat-stroked-button type="button" (click)="fileInput.click()">
              <mat-icon>attach_file</mat-icon>
              {{ fileName() || lang.t('ARTICLES_CHOOSE_FILE') }}
            </button>
            @if (fileError()) {
              <small class="article-create__file-error">{{ lang.t('ARTICLES_FILE_REQUIRED') }}</small>
            }
          </div>
        }

        <mat-form-field appearance="outline">
          <mat-label>{{ lang.t('COMMON_NAME') }}</mat-label>
          <input matInput formControlName="name" maxlength="255" />
          @if (form.controls.name.touched && form.controls.name.invalid) {
            <mat-error>{{ lang.t('ARTICLES_NAME_ERROR') }}</mat-error>
          }
        </mat-form-field>

        <mat-form-field appearance="outline">
          <mat-label>{{ lang.t('ARTICLES_FIELD_CATEGORY') }}</mat-label>
          <mat-select formControlName="category_id">
            @for (c of data.candidates; track c.id) {
              <mat-option [value]="c.id">{{ c.label }}</mat-option>
            }
          </mat-select>
          @if (form.controls.category_id.touched && form.controls.category_id.invalid) {
            <mat-error>{{ lang.t('ARTICLES_CATEGORY_REQUIRED') }}</mat-error>
          }
        </mat-form-field>

        <mat-form-field appearance="outline">
          <mat-label>{{ lang.t('ARTICLES_FIELD_DOCTYPE') }}</mat-label>
          <mat-select formControlName="doc_type">
            @for (t of docTypes; track t) {
              <mat-option [value]="t">{{ lang.t(docTypeKey(t)) }}</mat-option>
            }
          </mat-select>
        </mat-form-field>

        <mat-form-field appearance="outline">
          <mat-label>{{ lang.t('COMMON_DESCRIPTION') }}</mat-label>
          <textarea matInput formControlName="description" rows="2" maxlength="1000"></textarea>
        </mat-form-field>

        @if (source() === 'url') {
          <mat-form-field appearance="outline">
            <mat-label>{{ lang.t('ARTICLES_FIELD_CONTENT_TYPE') }}</mat-label>
            <input matInput formControlName="content_type" maxlength="100" />
          </mat-form-field>
        }
      </mat-dialog-content>
      <mat-dialog-actions align="end">
        <button mat-button type="button" (click)="dismiss()">{{ lang.t('COMMON_CANCEL') }}</button>
        <button mat-raised-button color="primary" type="submit" [disabled]="busy()">
          @if (busy()) {
            <mat-spinner diameter="18" />
          } @else {
            {{ lang.t('COMMON_CREATE') }}
          }
        </button>
      </mat-dialog-actions>
    </form>
  `,
  styles: [
    `
      :host {
        display: block;
      }
      mat-form-field {
        width: 100%;
      }
      .article-create__source-toggle {
        display: flex;
        gap: var(--space-2);
        margin-bottom: var(--space-4);

        button {
          flex: 1;
        }
      }
      .article-create__file {
        margin-bottom: var(--space-4);
        display: flex;
        flex-direction: column;
        gap: var(--space-1);
      }
      .article-create__file-error {
        color: var(--app-error);
      }
    `,
  ],
})
export class ArticleCreateDialog {
  protected readonly lang = inject(LangService);
  protected readonly data = inject<CreateDialogData>(MAT_DIALOG_DATA);
  private readonly ref = inject(MatDialogRef<ArticleCreateDialog>);
  private readonly fb = inject(FormBuilder);
  private readonly articlesApi = inject(ArticlesService);
  private readonly uploadsApi = inject(UploadsService);
  private readonly messages = inject(MessageService);

  protected readonly docTypes = DOC_TYPES;
  protected readonly source = signal<'url' | 'file'>('url');
  protected readonly busy = signal(false);
  protected readonly fileName = signal('');
  protected readonly fileError = signal(false);
  private selectedFile: File | null = null;

  protected readonly form = this.fb.nonNullable.group({
    url: [''],
    name: ['', [Validators.required, Validators.maxLength(255)]],
    category_id: [0, [Validators.required, Validators.min(1)]],
    doc_type: ['general'],
    description: ['', [Validators.maxLength(1000)]],
    content_type: ['text/html', [Validators.required, Validators.maxLength(100)]],
  });

  protected docTypeKey(t: (typeof DOC_TYPES)[number]): TranslationKey {
    return DOC_TYPE_KEYS[t];
  }

  protected setSource(value: 'url' | 'file'): void {
    this.source.set(value);
    // Switch nguồn thì field của nguồn kia không được chặn validate nữa.
    this.form.controls.url.clearValidators();
    if (value === 'url') {
      this.form.controls.url.setValidators([Validators.required, Validators.pattern(/^https?:\/\/.+/)]);
    }
    this.form.controls.url.updateValueAndValidity();
    this.fileError.set(false);
  }

  protected pickFile(input: HTMLInputElement): void {
    this.selectedFile = input.files?.[0] ?? null;
    this.fileName.set(this.selectedFile?.name ?? '');
    this.fileError.set(!this.selectedFile);
  }

  protected save(): void {
    if (this.form.invalid) {
      this.form.markAllAsTouched();
      return;
    }
    if (this.source() === 'file' && !this.selectedFile) {
      this.fileError.set(true);
      return;
    }
    const raw = this.form.getRawValue();
    const base = {
      name: raw.name,
      doc_type: raw.doc_type,
      description: raw.description,
      category_id: raw.category_id,
    };
    this.busy.set(true);

    if (this.source() === 'url') {
      this.articlesApi
        .create({ ...base, url: raw.url, content_type: raw.content_type || 'text/html' })
        .subscribe({
          next: (article) => this.finish(article),
          error: (err: unknown) => this.fail(err),
        });
      return;
    }

    // File: upload 2 bước — presign + PUT vào kho, rồi MẺI gửi storage_key.
    this.uploadsApi.upload(this.selectedFile!).subscribe({
      next: (ticket) => {
        this.articlesApi
          .create({ ...base, storage_key: ticket.key, content_type: ticket.content_type })
          .subscribe({
            next: (article) => this.finish(article),
            error: (err: unknown) => this.fail(err),
          });
      },
      error: (err: unknown) => this.fail(err),
    });
  }

  protected dismiss(): void {
    this.ref.close();
  }

  private finish(article: Article): void {
    this.busy.set(false);
    this.ref.close(article);
  }

  private fail(err: unknown): void {
    this.busy.set(false);
    if (err instanceof ApiRequestError) {
      this.messages.raw('error', err.message);
    } else {
      this.messages.error('LOGIN_ERRORS_GENERIC');
    }
  }
}
