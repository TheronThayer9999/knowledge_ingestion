import { inject, Injectable, signal } from '@angular/core';
import { MatDialog } from '@angular/material/dialog';
import { MatSnackBar } from '@angular/material/snack-bar';
import { takeUntil } from 'rxjs/operators';
import { firstValueFrom } from 'rxjs';
import { LangService, type TranslationKey } from '../../core/i18n/lang.service';
import { ConfirmDialog, type ConfirmDialogData } from './confirm-dialog';
import type { AppMessage, MessageType } from './message.model';

const MAX_HISTORY = 50;

/** Toast lỗi/cảnh báo giữ lâu hơn để kịp đọc */
const DURATION_MS: Record<MessageType, number> = {
  success: 3500,
  info: 3500,
  warning: 5000,
  error: 6000,
};

/** Snackbar có nút Hoàn tác: giữ lâu hơn để người dùng kịp đổi ý */
const UNDO_DURATION_MS = 6000;

/**
 * Lớp feedback dùng chung: mọi thông báo đi qua đây (MatSnackBar) và
 * xác nhận thao tác nguy hiểm đi qua confirm (MatDialog). Convention:
 * toast hiển thị ở góc trên bên phải (`verticalPosition: 'top'`, `horizontalPosition: 'end'`).
 */
@Injectable({ providedIn: 'root' })
export class MessageService {
  private readonly snackBar = inject(MatSnackBar);
  private readonly dialog = inject(MatDialog);
  private readonly lang = inject(LangService);
  private readonly items = signal<AppMessage[]>([]);

  /** 50 thông báo gần nhất — phục vụ debug, không phải nguồn hiển thị */
  readonly history = this.items.asReadonly();

  success(key: TranslationKey): void {
    this.notify('success', this.lang.t(key), key);
  }

  error(key: TranslationKey): void {
    this.notify('error', this.lang.t(key), key);
  }

  info(key: TranslationKey): void {
    this.notify('info', this.lang.t(key), key);
  }

  /**
   * Snackbar thành công kèm nút hành động (thường là "Hoàn tác").
   * Chỉ dùng khi thao tác đảo ngược được một cách an toàn (VD đổi thư mục);
   * thao tác không thể đảo (Xóa vĩnh viễn) thì dùng success() thường.
   * Kịch bản: bấm nút -> chạy onAction; hết hạn mà không bấm thì chẳng làm gì.
   */
  successWithAction(
    messageKey: TranslationKey,
    actionLabelKey: TranslationKey,
    onAction: () => void,
  ): void {
    this.record('success', this.lang.t(messageKey), messageKey);
    const ref = this.snackBar.open(this.lang.t(messageKey), this.lang.t(actionLabelKey), {
      duration: UNDO_DURATION_MS,
      verticalPosition: 'top',
      horizontalPosition: 'end',
    });
    ref
      .onAction()
      .pipe(takeUntil(ref.afterDismissed()))
      .subscribe(() => onAction());
  }

  warning(key: TranslationKey): void {
    this.notify('warning', this.lang.t(key), key);
  }

  /**
   * Hiển thị lỗi từ tầng API: message của server đã là tiếng Việt và
   * được phép lộ ra client (service wrap bằng NewCustomHttpError), nên
   * ưu tiên message đó; lỗi mạng/không xác định thì dùng key dự phòng.
   */
  errorFromApi(err: unknown, fallbackKey: TranslationKey = 'AUTH_ERROR_GENERIC'): void {
    const status = readNumberProperty(err, 'status');
    if (status === 0) {
      this.error('AUTH_ERROR_NETWORK');
      return;
    }
    const message = readStringProperty(err, 'message');
    if (message) {
      this.notify('error', message);
      return;
    }
    this.error(fallbackKey);
  }

  /** Hộp thoại xác nhận — trả về true khi người dùng đồng ý */
  async confirm(titleKey: TranslationKey, messageKey?: TranslationKey): Promise<boolean> {
    const data: ConfirmDialogData = {
      title: this.lang.t(titleKey),
      message: messageKey ? this.lang.t(messageKey) : '',
      confirmLabel: this.lang.t('COMMON_OK'),
      cancelLabel: this.lang.t('COMMON_CANCEL'),
    };
    const result = await firstValueFrom(
      this.dialog
        .open<ConfirmDialog, ConfirmDialogData, boolean>(ConfirmDialog, {
          data,
          width: '400px',
          autoFocus: 'dialog',
        })
        .afterClosed(),
    );
    return result === true;
  }

  private notify(type: MessageType, text: string, key?: TranslationKey): void {
    this.record(type, text, key);
    this.snackBar.open(text, undefined, {
      duration: DURATION_MS[type],
      verticalPosition: 'top',
      horizontalPosition: 'end',
    });
  }

  /** Ghi vào history dùng chung cho mọi loại snackbar (debug/audit) */
  private record(type: MessageType, text: string, key?: TranslationKey): void {
    const message: AppMessage = { type, text, key, at: new Date() };
    this.items.update((list) => [...list.slice(-(MAX_HISTORY - 1)), message]);
  }
}

function readNumberProperty(source: unknown, key: string): number | null {
  const value = (source as Record<string, unknown> | null | undefined)?.[key];
  return typeof value === 'number' ? value : null;
}

function readStringProperty(source: unknown, key: string): string | null {
  const value = (source as Record<string, unknown> | null | undefined)?.[key];
  return typeof value === 'string' && value.trim().length > 0 ? value : null;
}
