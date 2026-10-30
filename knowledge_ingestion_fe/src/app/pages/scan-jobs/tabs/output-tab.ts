import { Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { MatTooltipModule } from '@angular/material/tooltip';
import { ScanJobStore } from '../scan-job.store';

/**
 * Tab 3 — PDF chuẩn: xem và tải tệp kết quả.
 *
 * Khung xem là <iframe> trỏ thẳng URL presigned của kho: Chromium/PDFium tự lo
 * zoom, chuyển trang và in — không stream file qua API. Nguồn PDF trả đúng tệp
 * gốc, nguồn ảnh là ảnh bọc trong PDF một trang (BE chốt, xem buildOutput).
 */
@Component({
  selector: 'app-output-tab',
  imports: [MatButtonModule, MatIconModule, MatProgressBarModule, MatTooltipModule],
  templateUrl: './output-tab.html',
  styleUrl: './output-tab.scss',
})
export class OutputTab {
  protected readonly store = inject(ScanJobStore);
  protected readonly lang = this.store.lang;
}
