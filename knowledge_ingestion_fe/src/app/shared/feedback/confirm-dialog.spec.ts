import { TestBed } from '@angular/core/testing';
import { MAT_DIALOG_DATA, MatDialogRef } from '@angular/material/dialog';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ConfirmDialog, type ConfirmDialogData } from './confirm-dialog';

describe('ConfirmDialog', () => {
  const data: ConfirmDialogData = {
    title: 'Xác nhận đăng xuất',
    message: 'Bạn có chắc muốn đăng xuất?',
    confirmLabel: 'Đồng ý',
    cancelLabel: 'Hủy',
  };
  const dialogRef = { close: vi.fn() };

  beforeEach(() => {
    TestBed.resetTestingModule();
    dialogRef.close.mockClear();
    TestBed.configureTestingModule({
      imports: [ConfirmDialog],
      providers: [
        { provide: MAT_DIALOG_DATA, useValue: data },
        { provide: MatDialogRef, useValue: dialogRef },
      ],
    });
  });

  it('hiển thị tiêu đề, nội dung và 2 nút', () => {
    const fixture = TestBed.createComponent(ConfirmDialog);
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    expect(element.querySelector('h2')?.textContent).toContain('Xác nhận đăng xuất');
    expect(element.textContent).toContain('Bạn có chắc muốn đăng xuất?');

    const buttons = element.querySelectorAll('button');
    expect(buttons).toHaveLength(2);
  });

  it('bấm nút xác nhận thì close(true)', () => {
    const fixture = TestBed.createComponent(ConfirmDialog);
    fixture.detectChanges();

    (fixture.nativeElement as HTMLElement).querySelectorAll('button')[1].click();

    expect(dialogRef.close).toHaveBeenCalledWith(true);
  });
});
