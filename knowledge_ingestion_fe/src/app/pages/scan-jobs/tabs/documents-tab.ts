import { Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { MatSelectModule } from '@angular/material/select';
import { MatTooltipModule } from '@angular/material/tooltip';
import { ScanJobStore } from '../scan-job.store';

/**
 * Tab 1 — Tài liệu: chọn tệp nhận OCR.
 *
 * Ba bước theo đúng thứ tự thao tác (tệp → cài đặt → chạy). Cài đặt OCR phải đi
 * cùng request tạo job (`ScanJobService.create`) vì server lưu vào row rồi
 * worker đọc lại — form có mà không gửi thì đổi cài đặt không có tác dụng gì.
 */
@Component({
  selector: 'app-documents-tab',
  imports: [
    MatButtonModule,
    MatFormFieldModule,
    MatIconModule,
    MatProgressBarModule,
    MatSelectModule,
    MatTooltipModule,
  ],
  templateUrl: './documents-tab.html',
  styleUrl: './documents-tab.scss',
})
export class DocumentsTab {
  protected readonly store = inject(ScanJobStore);
  protected readonly lang = this.store.lang;
}
