import { TestBed } from '@angular/core/testing';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ChartLoader } from './chart-loader';
import { EchartsChart } from './echarts-chart';

/**
 * echarts THẬT không tải trong unit test (jsdom không có canvas, kéo ~900kB vào
 * test cũng vô nghĩa) — thay `ChartLoader` bằng fake qua TestBed, đây là cách
 * Angular unit-test builder cho phép (vi.mock import tương đối bị chặn).
 */
const handle = { setOption: vi.fn(), resize: vi.fn(), dispose: vi.fn() };
const createChart = vi.fn(() => handle);
const load = vi.fn(async () => createChart);

const BAR_SPEC: Record<string, unknown> = {
  xAxis: { type: 'category', data: ['NV01', 'NV02'] },
  series: [{ type: 'bar', data: [800000, 1200000] }],
};

function makeFixture(): ReturnType<typeof TestBed.createComponent<EchartsChart>> {
  const fixture = TestBed.createComponent(EchartsChart);
  fixture.componentRef.setInput('options', BAR_SPEC);
  fixture.componentRef.setInput('n', 9);
  fixture.componentRef.setInput('kind', 'bar');
  fixture.componentRef.setInput('caption', 'Nguồn: [^9] · Phân bố phụ cấp theo nhân viên');
  return fixture;
}

describe('EchartsChart', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({
      imports: [EchartsChart],
      providers: [{ provide: ChartLoader, useValue: { load } }],
    });
  });

  it('vẽ vào khung canvas rồi bỏ chữ đang tải', async () => {
    const fixture = makeFixture();
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    expect(element.querySelector('.chart-plot')?.getAttribute('role')).toBe('img');
    expect(element.textContent).not.toContain('Đang tải biểu đồ');
    expect(createChart).toHaveBeenCalledWith(element.querySelector('.chart-plot'), BAR_SPEC);
  });

  /**
   * Vẽ lại khi option đổi (mở lại phiên, backend trả spec mới): setOption lần
   * hai chứ KHÔNG init lại echarts — init lại là mất animation + tốn canvas.
   */
  it('đổi options sau khi vẽ thì setOption tiếp, không init lại', async () => {
    const fixture = makeFixture();
    fixture.detectChanges();
    await fixture.whenStable();

    const next: Record<string, unknown> = { series: [{ type: 'bar', data: [1, 2] }] };
    fixture.componentRef.setInput('options', next);
    fixture.detectChanges();

    expect(handle.setOption).toHaveBeenCalledWith(next);
    expect(createChart).toHaveBeenCalledTimes(1);
  });

  it('chip nguồn in theo số n, tiêu đề không lặp lại hai lần ngoài canvas', () => {
    const fixture = makeFixture();
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    expect(element.querySelector('.chart-source')?.textContent).toContain('Nguồn 9');
    expect(element.querySelector('.chart-title')).toBeNull();
  });

  /**
   * Canvas không vẽ được thì mất luôn spec.title nằm trong ảnh — phần caption
   * còn lại phải thành hàng chữ. Caption lạ (không theo khuôn `[^n] ·`) in
   * nguyên văn, không cắt mất chữ.
   */
  it('canvas không vẽ được thì tiêu đề thành hàng chữ, caption lạ in nguyên văn', async () => {
    load.mockRejectedValueOnce(new Error('chunk not found'));
    const fixture = makeFixture();
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
    expect(
      (fixture.nativeElement as HTMLElement).querySelector('.chart-title')?.textContent?.trim(),
    ).toBe('Phân bố phụ cấp theo nhân viên');

    load.mockRejectedValueOnce(new Error('chunk not found'));
    const odd = makeFixture();
    odd.componentRef.setInput('caption', 'Biểu đồ tự chế');
    odd.detectChanges();
    await odd.whenStable();
    odd.detectChanges();

    expect(
      (odd.nativeElement as HTMLElement).querySelector('.chart-title')?.textContent?.trim(),
    ).toBe('Biểu đồ tự chế');
  });

  it('bấm chip nguồn thì phát số nguồn lên trang chat', () => {
    const fixture = makeFixture();
    fixture.detectChanges();

    const emitted: number[] = [];
    fixture.componentInstance.citeClick.subscribe((n: number) => emitted.push(n));
    (fixture.nativeElement as HTMLElement)
      .querySelector<HTMLButtonElement>('.chart-source')
      ?.click();

    expect(emitted).toEqual([9]);
  });

  it('canvas có aria-label nói kiểu + tên + nguồn (screen reader không thấy canvas)', () => {
    const fixture = makeFixture();
    fixture.detectChanges();

    const label = (fixture.nativeElement as HTMLElement)
      .querySelector('.chart-plot')
      ?.getAttribute('aria-label');
    expect(label).toContain('Biểu đồ cột');
    expect(label).toContain('Phân bố phụ cấp theo nhân viên');
    expect(label).toContain('Nguồn 9');
  });

  it('echarts tải không về thì báo tại chỗ, không vỡ câu trả lời', async () => {
    load.mockRejectedValueOnce(new Error('chunk not found'));
    const fixture = makeFixture();
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    expect(element.textContent).toContain('Không tải được biểu đồ');
    expect(element.querySelector('.chart-source')).not.toBeNull();
  });

  it('rời trang thì dispose biểu đồ (canvas treo lại là rò bộ nhớ)', async () => {
    const fixture = makeFixture();
    fixture.detectChanges();
    await fixture.whenStable();

    fixture.destroy();

    expect(handle.dispose).toHaveBeenCalled();
  });
});
