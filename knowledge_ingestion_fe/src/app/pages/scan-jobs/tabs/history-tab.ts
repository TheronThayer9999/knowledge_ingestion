import { DatePipe } from '@angular/common';
import { Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatSelectModule } from '@angular/material/select';
import { MatTooltipModule } from '@angular/material/tooltip';
import type { ScanJobResponse } from '../../../core/scan/scan-job.service';
import { ScanJobStore } from '../scan-job.store';

/** Tab 4 — Lịch sử: mọi phiên đã tạo, lọc theo trạng thái, mở lại hoặc xoá. */
@Component({
  selector: 'app-history-tab',
  imports: [
    DatePipe,
    MatButtonModule,
    MatFormFieldModule,
    MatIconModule,
    MatSelectModule,
    MatTooltipModule,
  ],
  templateUrl: './history-tab.html',
  styleUrl: './history-tab.scss',
})
export class HistoryTab {
  protected readonly store = inject(ScanJobStore);
  protected readonly lang = this.store.lang;

  protected readonly filters = ['', 'pending', 'processing', 'done', 'failed', 'cancelled'];

  protected open(job: ScanJobResponse): void {
    this.store.open(job);
    this.store.showTab(1);
  }

  protected filterLabel(status: string): string {
    return this.lang.t(status ? this.store.statusKey(status) : 'SCAN_HISTORY_ALL');
  }
}
