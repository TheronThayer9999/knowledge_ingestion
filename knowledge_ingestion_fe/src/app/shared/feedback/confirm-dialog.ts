import { Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MAT_DIALOG_DATA, MatDialogModule, MatDialogRef } from '@angular/material/dialog';
import { TranslationKey, TranslationParams } from '../i18n/lang.model';
import { LangService } from '../i18n/lang.service';

export interface ConfirmDialogData {
  title: TranslationKey;
  message: TranslationKey;
  /** params thay thế vào message động (tên đối tượng sắp xóa...) */
  messageParams?: TranslationParams;
}

/** Popup confirm dùng chung — đóng góp true (xác nhận) / false (hủy). */
@Component({
  selector: 'app-confirm-dialog',
  imports: [MatDialogModule, MatButtonModule],
  template: `
    <h2 mat-dialog-title>{{ lang.t(data.title) }}</h2>
    <mat-dialog-content>{{ lang.t(data.message, data.messageParams) }}</mat-dialog-content>
    <mat-dialog-actions align="end">
      <button mat-button (click)="dismiss()">{{ lang.t('COMMON_CANCEL') }}</button>
      <button mat-button color="primary" (click)="accept()">{{ lang.t('COMMON_CONFIRM') }}</button>
    </mat-dialog-actions>
  `,
})
export class ConfirmDialog {
  protected readonly lang = inject(LangService);
  protected readonly data = inject<ConfirmDialogData>(MAT_DIALOG_DATA);
  private readonly ref = inject(MatDialogRef<ConfirmDialog>);

  protected accept(): void {
    this.ref.close(true);
  }

  protected dismiss(): void {
    this.ref.close(false);
  }
}
