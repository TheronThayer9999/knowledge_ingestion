import { TestBed } from '@angular/core/testing';
import { MatDialog } from '@angular/material/dialog';
import { MatSnackBar } from '@angular/material/snack-bar';
import { of, Subject } from 'rxjs';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { MessageService } from './message.service';

describe('MessageService', () => {
  const snackBar = { open: vi.fn() };
  const dialog = { open: vi.fn() };

  function setup(): MessageService {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({
      providers: [
        { provide: MatSnackBar, useValue: snackBar },
        { provide: MatDialog, useValue: dialog },
      ],
    });
    return TestBed.inject(MessageService);
  }

  beforeEach(() => {
    snackBar.open.mockClear();
    dialog.open.mockReset();
    dialog.open.mockReturnValue({ afterClosed: () => of(true) });
  });

  it('hiển thị toast ở góc trên bên phải và ghi vào history', () => {
    const service = setup();

    service.success('COMMON_OK');

    expect(snackBar.open).toHaveBeenCalledWith(
      'Đồng ý',
      undefined,
      expect.objectContaining({ verticalPosition: 'top', horizontalPosition: 'end' }),
    );
    expect(service.history()).toHaveLength(1);
    expect(service.history()[0]).toMatchObject({
      type: 'success',
      text: 'Đồng ý',
      key: 'COMMON_OK',
    });
  });

  it('giữ tối đa 50 thông báo trong history', () => {
    const service = setup();

    for (let i = 0; i < 55; i++) {
      service.info('APP_NAME');
    }

    expect(service.history()).toHaveLength(50);
  });

  it('successWithAction mở snackbar kèm nút và gọi onAction khi bấm', () => {
    const service = setup();
    const action$ = new Subject<void>();
    const dismissed$ = new Subject<void>();
    snackBar.open.mockReturnValueOnce({
      onAction: () => action$,
      afterDismissed: () => dismissed$,
    });
    const onAction = vi.fn();

    service.successWithAction('DOC_MOVE_UNDO', 'COMMON_UNDO', onAction);

    expect(snackBar.open).toHaveBeenCalledWith(
      'Đã chuyển tài liệu sang thư mục khác.',
      'Hoàn tác',
      expect.objectContaining({ verticalPosition: 'top', horizontalPosition: 'end' }),
    );
    expect(service.history().at(-1)).toMatchObject({ type: 'success', key: 'DOC_MOVE_UNDO' });

    // Subject phát đồng bộ tới subscriber của service — onAction chạy ngay
    action$.next();
    expect(onAction).toHaveBeenCalledTimes(1);

    // snackbar đã đóng thì action bị takeUntil, không gọi lại onAction
    dismissed$.next();
    action$.next();
    expect(onAction).toHaveBeenCalledTimes(1);
  });

  it('errorFromApi ưu tiên message do server trả về', () => {
    const service = setup();

    service.errorFromApi({ status: 401, code: 401, message: 'sai tài khoản hoặc mật khẩu' });

    expect(service.history().at(-1)).toMatchObject({
      type: 'error',
      text: 'sai tài khoản hoặc mật khẩu',
    });
  });

  it('errorFromApi dùng key lỗi mạng khi status = 0', () => {
    const service = setup();

    service.errorFromApi({ status: 0, code: 0, message: '' });

    expect(service.history().at(-1)?.key).toBe('AUTH_ERROR_NETWORK');
  });

  it('confirm trả về true khi người dùng đồng ý', async () => {
    const service = setup();

    const result = await service.confirm('COMMON_OK', 'AUTH_ERROR_GENERIC');

    expect(result).toBe(true);
    expect(dialog.open).toHaveBeenCalledWith(
      expect.anything(),
      expect.objectContaining({
        data: {
          title: 'Đồng ý',
          message: 'Đã có lỗi xảy ra, vui lòng thử lại.',
          confirmLabel: 'Đồng ý',
          cancelLabel: 'Hủy',
        },
      }),
    );
  });

  it('confirm trả về false khi người dùng hủy', async () => {
    const service = setup();
    dialog.open.mockReturnValueOnce({ afterClosed: () => of(false) });

    await expect(service.confirm('COMMON_CANCEL')).resolves.toBe(false);
  });
});
