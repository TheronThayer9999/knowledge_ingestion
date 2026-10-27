import { Injectable } from '@angular/core';
import { loadChart, type ChartFactory } from './echarts-loader';

/**
 * Cổng nạp biểu đồ để component tiêm được thay vì gọi hàm thẳng — unit test
 * (và sau này là renderer khác) thay thế bằng fake, không phải tải echarts
 * thật trong jsdom. Bản thân file này không import echarts ở đầu file.
 */
@Injectable({ providedIn: 'root' })
export class ChartLoader {
  load(): Promise<ChartFactory> {
    return loadChart();
  }
}
