import { Component, inject } from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MAT_DIALOG_DATA, MatDialogModule, MatDialogRef } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { CategoryNode } from '../../core/api/api.models';
import { LangService } from '../../shared/i18n/lang.service';

export interface CategoryDialogData {
  mode: 'create' | 'edit';
  /** create-child: id + tên cha hiện trên tiêu đề form. */
  parentId?: number;
  parentName?: string;
  /** edit: node đang sửa. */
  node?: CategoryNode;
  /** edit: ứng viên cha (đã loại trừ chính nó + con cháu), nhãn thụt lề theo depth. */
  candidates?: { id: number; label: string }[];
}

/** Kết quả form — component cha quyết định gọi create/update. */
export interface CategoryDialogResult {
  name: string;
  description: string;
  parentId?: number;
  moveToRoot: boolean;
}

/** Dialog tạo/sửa danh mục — 0 = "không có cha" (root / move_to_root). */
@Component({
  selector: 'app-category-dialog',
  imports: [
    MatDialogModule,
    ReactiveFormsModule,
    MatFormFieldModule,
    MatInputModule,
    MatSelectModule,
    MatButtonModule,
  ],
  template: `
    <h2 mat-dialog-title>
      {{ lang.t(data.mode === 'create' ? 'CATEGORY_DIALOG_CREATE' : 'CATEGORY_DIALOG_EDIT') }}
    </h2>
    <form [formGroup]="form" (ngSubmit)="save()">
      <mat-dialog-content>
        @if (data.mode === 'create' && data.parentName) {
          <p class="category-dialog__parent-hint">
            {{ lang.t('CATEGORY_PARENT') }}: {{ data.parentName }}
          </p>
        }
        <mat-form-field appearance="outline">
          <mat-label>{{ lang.t('COMMON_NAME') }}</mat-label>
          <input matInput formControlName="name" maxlength="100" />
          @if (form.controls.name.touched && form.controls.name.invalid) {
            <mat-error>{{ lang.t('CATEGORY_NAME_ERROR') }}</mat-error>
          }
        </mat-form-field>

        <mat-form-field appearance="outline">
          <mat-label>{{ lang.t('COMMON_DESCRIPTION') }}</mat-label>
          <textarea matInput formControlName="description" rows="2" maxlength="500"></textarea>
        </mat-form-field>

        @if (data.mode === 'edit') {
          <mat-form-field appearance="outline">
            <mat-label>{{ lang.t('CATEGORY_PARENT') }}</mat-label>
            <mat-select formControlName="parent">
              <mat-option [value]="0">{{ lang.t('CATEGORY_PARENT_ROOT') }}</mat-option>
              @for (c of data.candidates; track c.id) {
                <mat-option [value]="c.id">{{ c.label }}</mat-option>
              }
            </mat-select>
          </mat-form-field>
        }
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
      .category-dialog__parent-hint {
        margin: 0 0 var(--space-3);
        color: var(--color-text-secondary);
      }
    `,
  ],
})
export class CategoryDialog {
  protected readonly lang = inject(LangService);
  protected readonly data = inject<CategoryDialogData>(MAT_DIALOG_DATA);
  private readonly ref = inject(MatDialogRef<CategoryDialog>);
  private readonly fb = inject(FormBuilder);

  protected readonly form = this.fb.nonNullable.group({
    name: [this.data.node?.name ?? '', [Validators.required, Validators.minLength(1), Validators.maxLength(100)]],
    description: [this.data.node?.description ?? '', [Validators.maxLength(500)]],
    // 0 = root/move_to_root — edit seed cha hiện tại.
    parent: [this.data.node?.parent_id ?? 0],
  });

  protected save(): void {
    if (this.form.invalid) {
      return;
    }
    const raw = this.form.getRawValue();
    this.ref.close({
      name: raw.name,
      description: raw.description,
      parentId: raw.parent > 0 ? raw.parent : undefined,
      moveToRoot: this.data.mode === 'edit' && raw.parent === 0 && (this.data.node?.parent_id ?? 0) !== 0,
    });
  }

  protected dismiss(): void {
    this.ref.close();
  }
}
