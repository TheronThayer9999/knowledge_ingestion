import { Component, inject, type OnInit } from '@angular/core';
import { MatIconModule } from '@angular/material/icon';
import { MatTabsModule } from '@angular/material/tabs';
import { ScanJobStore } from './scan-job.store';
import { DocumentsTab } from './tabs/documents-tab';
import { HistoryTab } from './tabs/history-tab';
import { OutputTab } from './tabs/output-tab';
import { ReviewTab } from './tabs/review-tab';

/**
 * Trang Quét tài liệu — vỏ của 4 tab: Tài liệu / Xem & sửa / PDF chuẩn / Lịch sử.
 *
 * VÌ SAO 4 TAB CHỨ KHÔNG MỘT MÀN HÌNH: bốn việc (chọn tệp, đối chiếu chữ OCR,
 * xem PDF, tra lại phiên cũ) nhắm bốn đối tượng khác nhau, nhét chung một màn
 * hình thì khung xem PDF bị đẩy xuống dưới đáy và không còn chỗ cho bảng chữ.
 *
 * Trạng thái nằm ở `ScanJobStore` (cấp tại đây) nên 4 tab chia sẻ một vòng poll
 * duy nhất; nội dung tab nạp lười bằng `matTabContent` — mở trang chỉ dựng tab 1.
 */
@Component({
  selector: 'app-scan-jobs',
  imports: [MatIconModule, MatTabsModule, DocumentsTab, ReviewTab, OutputTab, HistoryTab],
  providers: [ScanJobStore],
  templateUrl: './scan-jobs.html',
  styleUrl: './scan-jobs.scss',
})
export class ScanJobs implements OnInit {
  protected readonly store = inject(ScanJobStore);
  protected readonly lang = this.store.lang;

  ngOnInit(): void {
    this.store.start();
  }
}
