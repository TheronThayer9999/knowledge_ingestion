import type { EChartsCoreOption } from 'echarts/core';

/**
 * Nạp ECharts chậm (Đợt 26).
 *
 * KHÔNG được import echarts ở đầu file nào khác: core ~900kB, rơi vào chunk
 * khởi động là vượt budget `initial` (600kB warn / 1MB error ở angular.json).
 * `loadChart()` dùng `import()` nên esbuild cắt echarts thành chunk riêng,
 * chỉ tải khi trang chat thật sự có biểu đồ.
 *
 * `import type` ở trên chỉ là type, bị xoá lúc biên dịch — không tốn bundle.
 */

/** Instance đã init, gói lại để component không phải biết kiểu riêng của echarts */
export interface ChartHandle {
  setOption: (option: Record<string, unknown>) => void;
  resize: () => void;
  dispose: () => void;
}

export type ChartFactory = (element: HTMLElement, option: Record<string, unknown>) => ChartHandle;

let pending: Promise<ChartFactory> | null = null;

/**
 * Module cần vẽ đúng hai khuôn option backend dựng ra (xem chart_spec.go):
 * bar/line cần grid + axis, pie cần legend, tất cả có tooltip.
 * Thêm kiểu biểu đồ mới ở backend thì phải đăng ký thêm module ở đây.
 */
export async function loadChart(): Promise<ChartFactory> {
  pending ??= (async () => {
    const [core, charts, components, renderers] = await Promise.all([
      import('echarts/core'),
      import('echarts/charts'),
      import('echarts/components'),
      import('echarts/renderers'),
    ]);
    core.use([
      charts.BarChart,
      charts.LineChart,
      charts.PieChart,
      components.GridComponent,
      components.LegendComponent,
      components.TooltipComponent,
      renderers.CanvasRenderer,
    ]);
    return (element, option) => {
      const instance = core.init(element);
      // Option do server cắt số từ chunk đã dẫn dựng nên FE không kiểm lại cấu trúc;
      // cổng kiểm số nằm ở backend (validateSpecNumbers), ở đây chỉ là cast.
      instance.setOption(option as EChartsCoreOption);
      return {
        setOption: (next) => instance.setOption(next as EChartsCoreOption),
        resize: () => instance.resize(),
        dispose: () => instance.dispose(),
      };
    };
  })();
  return pending;
}
