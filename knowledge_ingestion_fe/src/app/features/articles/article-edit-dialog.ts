import { Component, inject } from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MAT_DIALOG_DATA, MatDialogModule, MatDialogRef } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { Article, UpdateArticleRequest } from '../../core/api/api.models';
import { LangService } from '../../shared/i18n/lang.service';

export interface ArticleEditDialogData {
  article: Article;
  candidates: { id: number; label: string }[];
}

/** Sửa metadata bài — nguồn (url/storage_key) là identity, BE không cho đổi. */
@Component({
  selector: 'app-article-edit-dialog',
  imports: [
    MatDialogModule,
    ReactiveFormsModule,
    MatFormFieldModule,
    MatInputModule,
    MatSelectModule,
    MatButtonModule,
  ],
  template: `
    <h2 mat-dialog-title>{{ lang.t('ARTICLES_EDIT_TITLE') }}</h2>
    <form [formGroup]="form" (ngSubmit)="save()">
      <mat-dialog-content>
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
        </mat-form-field>

        <mat-form-field appearance="outline">
          <mat-label>{{ lang.t('ARTICLES_FIELD_DOCTYPE') }}</mat-label>
          <mat-select formControlName="doc_type">
            @for (t of docTypes; track t) {
              <mat-option [value]="t">{{ t }}</mat-option>
            }
          </mat-select>
        </mat-form-field>

        <mat-form-field appearance="outline">
          <mat-label>{{ lang.t('ARTICLES_FIELD_CONTENT_TYPE') }}</mat-label>
          <input matInput formControlName="content_type" maxlength="100" />
        </mat-form-field>

        <mat-form-field appearance="outline">
          <mat-label>{{ lang.t('COMMON_DESCRIPTION') }}</mat-label>
          <textarea matInput formControlName="description" rows="2" maxlength="1000"></textarea>
        </mat-form-field>
      </mat-dialog-content>
      <mat-dialog-actions align="end">
        <button mat-button type="button" (click)="dismiss()">{{ lang.t('COMMON_CANCEL') }}</button>
        <button mat-raised-button color="primary" type="submit" [disabled]="form.invalid">
          {{ lang.t('COMMON_SAVE') }}
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
    `,
  ],
})
export class ArticleEditDialog {
  protected readonly lang = inject(LangService);
  protected readonly data = inject<ArticleEditDialogData>(MAT_DIALOG_DATA);
  private readonly ref = inject(MatDialogRef<ArticleEditDialog>);
  private readonly fb = inject(FormBuilder);

  protected readonly docTypes = ['general', 'laws', 'table', 'paper', 'book', 'qa', 'manual'] as const;

  protected readonly form = this.fb.nonNullable.group({
    name: [this.data.article.name, [Validators.required, Validators.maxLength(255)]],
    category_id: [this.data.article.category_id, [Validators.required, Validators.min(1)]],
    doc_type: [this.data.article.doc_type || 'general'],
    content_type: [this.data.article.content_type, [Validators.required, Validators.maxLength(100)]],
    description: [this.data.article.description ?? '', [Validators.maxLength(1000)]],
  });

  protected save(): void {
    if (this.form.invalid) {
      return;
    }
    const raw = this.form.getRawValue();
    const req: UpdateArticleRequest = {
      name: raw.name,
      category_id: raw.category_id,
      doc_type: raw.doc_type,
      content_type: raw.content_type,
      description: raw.description,
    };
    this.ref.close(req);
  }

  protected dismiss(): void {
    this.ref.close();
  }
}
