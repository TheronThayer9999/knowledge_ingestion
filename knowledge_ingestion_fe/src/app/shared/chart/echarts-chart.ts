import {
  Component,
  ElementRef,
  OnDestroy,
  OnInit,
  effect,
  inject,
  input,
  output,
  signal,
  viewChild,
} from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { LangService, type TranslationKey } from '../../core/i18n/lang.service';
import { ChartLoader } from './chart-loader';
import type { ChartHandle } from './echarts-loader';

/** kind backend sinh ra (chart_spec.go); kind lạ không có nhãn thì in nguyên văn */
const KIND_KEYS: Record<string, TranslationKey> = {
  bar: 'CHAT_CHART_KIND_BAR',
  line: 'CHAT_CHART_KIND_LINE',
  pie: 'CHAT_CHART_KIND_PIE',
};

/**
 * Một biểu đồ trong câu trả lời chat (Đợt 26).
 *
 * FE chỉ vẽ: `options` là ECharts option do backend cắt số từ chunk đã dẫn và
 * đã qua cổng kiểm `validateSpecNumbers`, nên ở đây không tính toán, không
 * suy đoán thêm con số nào. echarts được tải chậm qua `ChartLoader` (xem
 * echarts-loader.ts) để không kéo ~900kB vào chunk khởi động.
 */
@Component({
  selector: 'app-chart',
  imports: [MatButtonModule, MatIconModule],
  templateUrl: './echarts-chart.html',
  styleUrl: './echarts-chart.scss',
})
export class EchartsChart implements OnInit, OnDestroy {
  protected readonly lang = inject(LangService);
  private readonly loader = inject(ChartLoader);

  readonly options = input.required<Record<string, unknown>>();
  readonly n = input.required<number>();
  readonly caption = input('');
  readonly kind = input('');

  /** Bấm chip nguồn dưới biểu đồ — trang chat nhảy tới đúng mục trong danh sách nguồn */
  readonly citeClick = output<number>();

  protected readonly ready = signal(false);
  protected readonly failed = signal(false);

  private readonly canvas = viewChild.required<ElementRef<HTMLDivElement>>('canvas');
  private handle?: ChartHandle;
  private observer?: ResizeObserver;
  private destroyed = false;

  constructor() {
    // Cùng một bong bóng có thể đổi option (mở lại phiên, vẽ lại): vẽ lại tại chỗ
    // thay vì init lại echarts
    effect(() => {
      const option = this.options();
      this.handle?.setOption(option);
    });
  }

  public async ngOnInit(): Promise<void> {
    try {
      const factory = await this.loader.load();
      if (this.destroyed) {
        return;
      }
      const element = this.canvas().nativeElement;
      this.handle = factory(element, this.options());
      this.ready.set(true);
      this.observeResize(element);
    } catch {
      // echarts không tải về được (mạng chập) — báo tại chỗ, câu trả lời vẫn đọc được
      this.failed.set(true);
    }
  }

  public ngOnDestroy(): void {
    this.destroyed = true;
    this.observer?.disconnect();
    this.observer = undefined;
    this.handle?.dispose();
    this.handle = undefined;
  }

  /**
   * Caption backend trả về dạng `Nguồn: [^9] · <tiêu đề>`. Tiêu đề đã được
   * echarts vẽ trong canvas (spec.title) nên chỗ này chỉ giữ phần chữ dùng cho
   * aria-label và hàng chữ dự phòng khi canvas không vẽ được. Số nguồn đã có
   * nút riêng. Không khớp khuôn thì in nguyên văn để không mất chữ.
   */
  protected captionText(): string {
    const caption = this.caption();
    const marker = caption.indexOf(`[^${this.n()}]`);
    if (marker < 0) {
      return caption;
    }
    const separator = caption.indexOf('·', marker);
    return separator < 0 ? '' : caption.slice(separator + 1).trim();
  }

  protected kindLabel(): string {
    const key = KIND_KEYS[this.kind()];
    return key ? this.lang.t(key) : this.kind();
  }

  /** Canvas là ảnh với screen reader nên mô tả bằng lời: kiểu + tên + nguồn */
  protected ariaLabel(): string {
    const title = this.captionText();
    const described = `${this.lang.t('CHAT_CHART_ARIA')} ${this.kindLabel()}`;
    const named = title === '' ? described : `${described}: ${title}`;
    return `${named} — ${this.lang.t('CHAT_CHART_SOURCE')} ${this.n()}`;
  }

  /** Bong bóng co giãn theo panel/sidebar — echarts không tự biết nên phải báo */
  private observeResize(element: HTMLElement): void {
    if (typeof ResizeObserver === 'undefined') {
      return;
    }
    this.observer = new ResizeObserver(() => this.handle?.resize());
    this.observer.observe(element);
  }
}
