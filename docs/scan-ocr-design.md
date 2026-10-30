# Thiết kế lại: "Quét tài liệu" (OCR)

> **CẬP NHẬT 2026-10-30 — đọc trước.** Tài liệu này mô tả bố cục MỘT MÀN HÌNH có
> khung xem PDF và nhắc "PDF searchable". Thiết kế đã chốt lại thành **4 tab**
> (Tài liệu / Xem & sửa / PDF chuẩn / Lịch sử) và **bỏ hẳn lớp chữ ẩn**: kết quả
> là tệp PDF (nguồn PDF trả nguyên bản, nguồn ảnh bọc một trang), chữ OCR chỉ
> xem/sửa ở tab 2 — gõ Ctrl+F trong PDF **không** tìm được. Màu/khoảng cách/token
> bên dưới vẫn đúng và vẫn dùng. Nguồn sự thật của hành vi hiện tại:
> `plans/2026-10-30-dot-31-ocr-searchable-pdf-4-tab.md` (phần BE) +
> `knowledge_ingestion_fe/src/app/pages/scan-jobs/`.

> Màn hình `/app/scan-jobs`. Stack: Angular + Angular Material (M3), token trong `src/styles/tokens/*`. Ngôn ngữ chính tiếng Việt. Lưới 8px.

## 1. Token màu & kiểu chữ & khoảng cách

### 1.1 Bảng màu (alias của `--mat-sys-*`, tự đổi theo palette/theme)

| Token | Vai trò | Light (azure) | Dark (azure) | Ghi chú |
|---|---|---|---|---|
| `--color-page-background` | Nền trang | `#f4f3f6` | `#1a1b1f` | trang nhạt hơn card |
| `--color-card-background` | Card/panel | `#ffffff` | `#2a2b30` | nổi trên nền trang |
| `--color-on-surface` | Chữ chính | `#1a1b1f` | `#e3e2e6` | không đen tuyệt đối |
| `--color-muted` | Chữ phụ | `#44474e` | `#c4c6cf` | ≥ 4.5:1 trên card |
| `--color-border` | Viền nhạt | `#d8dae1` | `#44474e` | mọi đường kẻ |
| `--color-primary` | Màu nhấn chính | `#005ac1` | `#a8c7fa` | nút chính, nav active |
| `--color-on-primary` | Chữ trên primary | `#ffffff` | `#002f66` | |
| `--color-danger` | Lỗi | `#ba1a1a` | `#ffb4ab` | |
| `--color-error-container` | Nền lỗi | `#ffdad6` | `#93000a` | |
| `--color-success` | Thành công (mới) | `#1e7e34` | `#81c995` | chỉ dùng cho badge "Hoàn tất" |
| `--color-warning` | Cảnh báo (mới) | `#b26a00` | `#ffb74d` | "Lỗi một phần" |
| `--color-accent-soft` | Nền nav đang chọn | primary@14% | primary@14% | pill như Gmail |
| `--color-accent-faint` | Hover | primary@7% | primary@7% | |
| `--color-scrim` | Overlay | scrim@32% | scrim@32% | drawer/modal |

> Mọi màu mới (`--color-success`, `--color-warning`) phải khai báo trong `tokens/_color.scss` và map qua `--mat-sys-*` khi có thể; nếu palette M3 không có thì dùng `color-mix` từ primary/error.

### 1.2 Typography (font Be Vietnam Pro cho tiếng Việt, fallback Noto Sans)

Đổi font chính từ Roboto sang `Be Vietnam Pro` (subset `latin` + `vietnamese`, weight 400/500/600/700) để dấu tiếng Việt đầy đủ và thẩm mỹ hơn. Giữ `--font-serif` (Times New Roman) cho markdown AI.

| Token | Size/Line | Weight | Dùng cho |
|---|---|---|---|
| `--text-style-xs` | 12px / 16px | 400 | caption, badge, meta |
| `--text-style-sm` | 14px / 20px | 400 | body mặc định |
| `--text-style-md` | 16px / 24px | 500 | tiêu đề section, label form |
| `--text-style-lg` | 20px / 28px | 600 | tiêu đề trang |
| `--text-style-xl` | 24px / 32px | 700 | tiêu đề lớn (empty state) |

### 1.3 Spacing (lưới 8px)

| Token | Giá trị |
|---|---|
| `--space-1` | 4px |
| `--space-2` | 8px |
| `--space-3` | 12px |
| `--space-4` | 16px |
| `--space-5` | 24px |
| `--space-6` | 32px |
| `--space-7` | 48px |
| `--space-8` | 64px |

Bo góc: `--radius-xs` 4px, `--radius-sm` 8px, `--radius-md` 12px, `--radius-lg` 16px, `--radius-full` 9999px. Bóng: `--elevation-1/2/3` (theo M3).

## 2. Bố cục

### 2.1 Shell (desktop ≥ 1024px)

```
┌────────────┬──────────────────────────────────────────────┐
│ Sidebar    │ Top bar: [Logo] [Search global] [Lang] [Theme] [Avatar] │
│ (240px)    ├──────────────────────────────────────────────┤
│            │  Content: 2 cột (grid 12, gap 24px)          │
│            │  ┌──────────────┐ ┌──────────────────────┐  │
│            │  │ Left 4/12    │ │ Right 8/12 (viewer)  │  │
│            │  │ (controls)   │ │ (PDF viewer)         │  │
│            │  └──────────────┘ └──────────────────────┘  │
└────────────┴──────────────────────────────────────────────┘
```

- Sidebar rộng 240px, collapse xuống 72px (chỉ icon) khi toggle; nhóm "Không gian làm việc" / "Tài khoản" có tiêu đề 12px xám.
- Mục đang chọn: nền `--color-accent-soft`, chữ `--color-primary`, bo góc 8px, icon filled.
- Top bar cao 56px, sticky, nền `--color-card-background`, viền dưới `--color-border`.
- Content padding 24px; viewer chiếm toàn bộ chiều cao còn lại (`calc(100vh - 56px - 48px)`), không cuộn trang khi preview.

### 2.2 Mobile (< 768px)

- Sidebar → drawer trượt từ trái (scrim 32%), hamburger ở top bar.
- Content xếp chồng: controls trước, viewer sau (chiều cao 60vh).
- Nút hành động chính "Bắt đầu OCR" cố định ở đáy màn hình (sticky bottom bar, cao 64px, nền card, viền trên).

### 2.3 Tablet (768–1023px)

- Sidebar collapse icon-only (72px), content 2 cột nhưng tỷ lệ 5/7.

## 3. Component & cách đặt tên Angular

Prefix feature: `scan-` (standalone component). Đặt trong `src/app/pages/scan-jobs/` và `src/app/shared/scan/`.

| Component | Selector | Vai trò |
|---|---|---|
| `ScanJobsPage` | `app-scan-jobs` | Container, orchestrate state |
| `ScanDropzone` | `app-scan-dropzone` | Vùng kéo-thả + danh sách tệp |
| `ScanFileItem` | `app-scan-file-item` | 1 dòng tệp (tên, size, xóa) |
| `ScanSettings` | `app-scan-settings` | Ngôn ngữ OCR, DPI, chất lượng |
| `ScanProgress` | `app-scan-progress` | Thanh tiến trình theo tệp + hủy |
| `ScanStatusBanner` | `app-scan-status-banner` | Thông báo trạng thái/lỗi |
| `ScanPdfViewer` | `app-scan-pdf-viewer` | Thumbnail + zoom + trang + fullscreen |
| `ScanEmptyState` | `app-scan-empty-state` | Empty state hướng dẫn |

Service: `ScanJobService` (đã có). State: dùng Angular signals trong `ScanJobsPage` (file, jobs, busy, error, previewUrl).

## 4. Vùng điều khiển (cột trái)

Thứ tự từ trên xuống:

1. **Tiêu đề trang**: `H1` "Quét tài liệu → PDF searchable" + subtitle muted.
2. **ScanDropzone**: vùng kéo-thả lớn (min-height 160px), border dashed `--color-border`, hover → `--color-accent-faint`, icon upload + text "Kéo thả ảnh/PDF vào đây hoặc bấm Chọn tệp". Hỗ trợ nhiều tệp; mỗi tệp hiện `ScanFileItem`: icon theo loại, tên (ellipsis), dung lượng (formatBytes), nút xóa (icon button). Lỗi validate (sai định dạng, > giới hạn) hiện ngay dưới item màu danger.
3. **ScanSettings**: 
   - Ngôn ngữ OCR: segmented control 3 nút "Tiếng Việt" / "Tiếng Anh" / "Việt + Anh" (default Việt+Anh).
   - Chất lượng: slider "Độ phân giải (DPI)" 150/200/300 (default 200) + select "Chế độ" (Tự động / Chỉ PDF có text layer / Bắt buộc OCR).
   - Collapsible "Nâng cao": PSM mode, min confidence.
4. **Nút hành động**: 
   - Primary (filled, màu primary, full width): "Bắt đầu OCR" — disabled khi chưa có tệp hoặc đang xử lý.
   - Secondary (outlined): "Hủy" (chỉ khi đang xử lý).
   - Tertiary (text): "Xóa tất cả".
5. **ScanProgress**: khi đang xử lý, danh sách job với progress bar + "Trang X/Y" + nút hủy từng job.
6. **ScanStatusBanner**: thông báo thành công/lỗi.

## 5. Trình xem PDF (cột phải)

- Container card, header bar (40px): tên file + số trang + zoom controls (− / % / +) + nút "Toàn màn hình" + "Tải PDF searchable" (chỉ bật khi done).
- Body: iframe (Chrome PDFium) chiếm phần còn lại; khi chưa có PDF → empty state "Chưa có bản xem trước".
- Sidebar thumbnail (collapsible, rộng 120px bên trái viewer): grid thumbnail trang, click để nhảy trang; có nút thu gọn (chevron). Ẩn mặc định trên mobile.
- Phím tắt: `+`/`-` zoom, `←`/`→` chuyển trang, `F` toàn màn hình, `Esc` thoát.

## 6. 5 trạng thái

### S1 — Chưa có tệp (empty state)
- Dropzone lớn với illustration + text "Kéo thả ảnh hoặc PDF scan vào đây" + nút "Chọn tệp".
- Settings disable (opacity 0.5).
- Viewer empty state "Chưa có bản xem trước".
- Không có progress.

### S2 — Đã chọn tệp, chờ xử lý
- Dropzone thu nhỏ, danh sách tệp hiển thị.
- Settings bật.
- Nút "Bắt đầu OCR" enabled.
- Viewer empty.

### S3 — Đang xử lý
- Dropzone disable (không cho thêm tệp trong lúc chạy; hoặc cho thêm vào hàng đợi — chọn disable để đơn giản).
- Settings disable.
- Nút primary → "Đang xử lý…" + spinner, disable; nút "Hủy" hiện.
- Progress bar per file + "Trang X/Y".
- Viewer: skeleton + text "Đang OCR, vui lòng chờ…".
- Khi chờ lâu (>30s): hiện hint "Việc này có thể mất vài phút với tài liệu dài. Bạn có thể để tab chạy nền."

### S4 — Hoàn tất
- Banner success "OCR hoàn tất — N trang, M từ".
- Nút "Tải PDF searchable" (primary) + "Xem trước" đã tự hiển thị trong viewer.
- Dropzone reset để xử lý tệp mới; nút "Làm lại" (secondary).

### S5 — Lỗi một phần / toàn bộ
- Banner error: "Tệp X, trang Y: không đọc được (ảnh mờ). Thử tăng DPI hoặc chọn vùng rõ hơn." kèm nút "Thử lại trang này" và "Bỏ qua".
- Tệp lỗi có badge đỏ + tooltip lý do; tệp OK vẫn preview.
- Nút "Tải PDF searchable" chỉ bật nếu ≥1 trang OK; nếu 0 trang OK → disable + gợi ý.

## 7. Thông báo lỗi (chuẩn)

Format: `{Tệp} — trang {n}: {lý do}. {gợi ý}`. Ví dụ:
- "scan_01.pdf — trang 3: ảnh quá mờ (độ tin cậy OCR 42%). Tăng DPI lên 300 hoặc quét lại vùng này."
- "photo.jpg: định dạng không hỗ trợ. Chuyển sang PNG/PDF."
- "report.pdf: vượt 50MB. Chia nhỏ tệp hoặc nén ảnh."

Không dùng "lỗi 0" / mã lỗi trần. Mã lỗi nội bộ chỉ log, không hiện UI.

## 8. UX notes

- Thứ tự thao tác: chọn tệp → (tùy chọn) chỉnh settings → Bắt đầu OCR → theo dõi progress → preview → tải.
- Phím tắt toàn cục: `Ctrl/Cmd+O` mở chọn tệp; `Enter` bắt đầu OCR (khi có tệp); `Esc` hủy dialog/fullscreen.
- Trạng thái chờ lâu: progress bar determinate theo trang; nếu không biết tổng trang (PDF chưa render) → indeterminate + text "Đang chuẩn bị…".
- Lưu cài đặt OCR cuối cùng vào localStorage (ngôn ngữ, DPI) để lần sau mặc định.
- Tự động poll mỗi 2s; dừng khi done/failed/cancelled; hiện toast khi tab ở nền và job xong.
- Accessibility: mọi icon button có `aria-label`; focus ring `--focus-ring`; contrast WCAG AA (đã đảm bảo qua token muted đậm hơn).
- Mobile: nút chính sticky bottom, không che nội dung (padding-bottom 80px cho scroll container).

## 9. Mockup (mô tả text)

Do không sinh ảnh, dưới đây là bố cục ASCII đại diện:

**Desktop — S3 (đang xử lý), light:**
```
┌──────────┬────────────────────────────────────────────────────┐
│ SIDEBAR  │ TOPBAR: Knowledge Ingestion  [Search] [VI] [☀] [👤] │
│          ├────────────────────────────────────────────────────┤
│ WS       │  Quét tài liệu → PDF searchable                    │
│ • Tổng   │  ┌────────────────┐ ┌──────────────────────────┐ │
│ • Tài    │  │ [Dropzone]     │ │ ┌────────────────────┐   │ │
│ • PDF ✓  │  │ scan_01.pdf 2MB│ │ │  PDF Viewer  1/5   │   │ │
│ • Chat   │  │ [Lang: VI+EN] │ │ │  [====progress====] │   │ │
│          │  │ [DPI: 200 ▾]   │ │ │  trang 2/5          │   │ │
│ ACCT     │  │ [Bắt đầu OCR]  │ │ └────────────────────┘   │ │
│ • Cài    │  │ [Hủy]          │ │ [Thumbnail ▾]            │ │
│ • Hồ sơ  │  └────────────────┘ └──────────────────────────┘ │
└──────────┴────────────────────────────────────────────────────┘
```

**Mobile — S1 (empty), dark:** drawer đóng; dropzone full width; bottom bar cố định "Chọn tệp"; viewer 60vh empty state.

## 10. Checklist triển khai

- [ ] Thêm font Be Vietnam Pro vào `_fonts.scss` (latin + vietnamese, weight 400/500/600/700).
- [ ] Thêm token `--color-success`, `--color-warning` vào `_color.scss`.
- [ ] Tạo 8 component scan-* ở trên.
- [ ] Refactor `scan-jobs.ts` container dùng signals + các component con.
- [ ] Thêm i18n key cho toàn bộ nhãn (không hardcode tiếng Việt trong template).
- [ ] Implement polling + toast nền.
- [ ] Test responsive + dark mode + a11y.

## 11. Ghi chú về font

`Be Vietnam Pro` hỗ trợ đầy đủ dấu tiếng Việt và có weight 400–700. Cài `@fontsource/be-vietnam-pro`. Nếu muốn giữ Roboto, chỉ cần đảm bảo subset vietnamese đã import (hiện đã có) — nhưng Be Vietnam Pro đẹp hơn cho tiếng Việt. Quyết định cuối thuộc team; token typography không phụ thuộc font cụ thể.