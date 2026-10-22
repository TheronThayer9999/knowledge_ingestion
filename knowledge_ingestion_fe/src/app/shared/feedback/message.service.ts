import { Injectable, computed, inject, signal } from '@angular/core';
import { MatDialog } from '@angular/material/dialog';
import { MatSnackBar } from '@angular/material/snack-bar';
import { map, Observable } from 'rxjs';
import { TranslationKey, TranslationParams } from '../i18n/lang.model';
import { LangService } from '../i18n/lang.service';
import { ConfirmDialog } from './confirm-dialog';
import { AppMessage, MessageType } from './message.model';

const TOAST_DURATION_MS = 3000;
const HISTORY_LIMIT = 50;

/**
 * TRUNG TÂM MESSAGE: mọi phản hồi tới người dùng (toast + popup confirm)
 * đều bắn ra từ đây — component không tự vẽ block inline riêng nữa.
 * - notify(): toast MatSnackBar, text dịch qua key i18n lúc hiển thị.
 * - confirm(): popup MatDialog, trả Observable<boolean>.
 * - messages(): signal lịch sử các message đã bắn (debug/audit, giữ MAX 50).
 */
@Injectable({ providedIn: 'root' })
export class MessageService {
  private readonly snackbar = inject(MatSnackBar);
  private readonly dialog = inject(MatDialog);
  private readonly lang = inject(LangService);

  private readonly history = signal<AppMessage[]>([]);
  readonly messages = computed(() => this.history());

  notify(type: MessageType, key: TranslationKey, params?: TranslationParams): void {
    this.history.update((list) => [...list.slice(-HISTORY_LIMIT + 1), { type, key, params }]);
    this.snackbar.open(this.lang.t(key, params), undefined, {
      duration: TOAST_DURATION_MS,
      horizontalPosition: 'center',
      verticalPosition: 'top',
      panelClass: ['app-toast', `app-toast--${type}`],
    });
  }

  success(key: TranslationKey, params?: TranslationParams): void {
    this.notify('success', key, params);
  }

  error(key: TranslationKey, params?: TranslationParams): void {
    this.notify('error', key, params);
  }

  info(key: TranslationKey, params?: TranslationParams): void {
    this.notify('info', key, params);
  }

  warning(key: TranslationKey, params?: TranslationParams): void {
    this.notify('warning', key, params);
  }

  /** Popup xác nhận dùng chung — true khi người dùng bấm Confirm. */
  confirm(title: TranslationKey, message: TranslationKey): Observable<boolean> {
    return this.dialog
      .open(ConfirmDialog, { data: { title, message }, width: '360px', autoFocus: true })
      .afterClosed()
      .pipe(map((result) => result === true));
  }
}
