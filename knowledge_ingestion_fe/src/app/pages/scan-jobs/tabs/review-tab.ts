import { Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { MatTooltipModule } from '@angular/material/tooltip';
import { ScanJobStore } from '../scan-job.store';

/**
 * Tab 2 — Xem & sửa: ảnh trang gốc + chữ OCR của đúng trang đó.
 *
 * Ảnh do BE render lười (pdftoppm, có semaphore chặn số lượt đồng thời) và trả
 * URL presigned; box từng từ là toạ độ chuẩn hoá 0..1 nên overlay không phụ
 * thuộc DPI hay zoom trình duyệt. Từ có độ tự tin thấp được tô đỏ để người dùng
 * biết chỗ nào cần soi — chi tiết từng dòng nằm ở panel chữ bên phải.
 */
@Component({
  selector: 'app-review-tab',
  imports: [MatButtonModule, MatIconModule, MatProgressBarModule, MatTooltipModule],
  templateUrl: './review-tab.html',
  styleUrl: './review-tab.scss',
})
export class ReviewTab {
  protected readonly store = inject(ScanJobStore);
  protected readonly lang = this.store.lang;
}
