# Đợt 31 — OCR tài liệu scan → PDF (tab Quét, 4 tab)

Ngày: 2026-10-30 · Trạng thái: **đã code xong (BE + FE, đã verify)** · Nối: trang tab Quét (`knowledge_ingestion_fe/src/app/pages/scan-jobs/` + `src/controller/services/scan_job_*.go`) — đợt này biến "xem trước OCR" thành hàng đợi job thật có 4 tab.

> ## ⚠️ CHỐT LẠI — đọc trước khi theo tài liệu này
>
> Tài liệu dưới đây là **bản thiết kế ban đầu**, giữ nguyên để tra lịch sử. Ba quyết định đã đổi khi làm thật, phần "Searchable PDF" trong tài liệu **không còn đúng**:
>
> 1. **BỎ HẲN lớp chữ ẩn (searchable PDF).** Yêu cầu chốt lại: "nó đơn giản là OCR chữ in mà, kiểu như convert ra PDF thôi". Hệ quả: package `src/common/searchpdf` **đã xoá**; kết quả job = tệp **PDF**: nguồn PDF → output trỏ thẳng `SourceKey` (không render/copy), nguồn JPG/PNG → bọc 1 ảnh vào PDF một trang bằng `fpdf` (`buildOutput` → `imagePagePDF`). Chữ OCR vẫn lưu từ+box+confidence theo trang nhưng **chỉ để tab 2 hiển thị/sửa**; gõ Ctrl+F trong PDF **không** tìm được.
> 2. **XOÁ endpoint đồng bộ `POST /scan/ocr`** (kèm `scan_ocr_service.go`, `scan_api.go`, `dtos/scan_dto.go`) — trái với giả định 7 bên dưới. Chỉ còn hàng đợi `/scan/jobs`.
> 3. **KHÔNG nhận WEBP** (`scanExts`, `scanMIME`, `accept` của FE, copy i18n): fpdf chỉ nhúng được JPEG/PNG và `golang.org/x/image` không có trong `go.mod`, nên nhận webp là không có đường nào sinh PDF.
>
> Hai thứ **thêm** so với thiết kế: janitor `purge-scan-jobs` (`IScanJobJanitor.PurgeExpired`, xoá object trước rồi `DeleteIDs` theo lô — luồng tạm mà không dọn thì rác nằm vĩnh viễn) và semaphore `SCAN_RENDER_CONCURRENCY=2` chặn số lượt `pdftoppm` đồng thời khi nhiều người mở tab Xem & sửa.
>
> Hai việc **chưa làm** (cố ý): sửa text bằng tay (PATCH từng từ/dòng) và "Đẩy vào kho" — xem mục B.7/A.2, coi là V2. Nguồn sự thật của hành vi hiện tại: code + `AGENTS.md` §"Tab Quét".
Tham chiếu nghiệp vụ: `D:\code\work\autoai` (Flask + React, `backend/api/ocr_tools_api.py`, `backend/rag/ocr_tools.py`, `frontend/.../components/ocr/OcrTools.tsx`).

---

## 0. Giả định nêu trước (không hỏi lại)

1. **"4 tab" là yêu cầu thiết kế, không phải mô tả autoai.** Đã đọc kỹ source: autoai **chỉ có MỘT tab** thật — `OcrTools.tsx:577-596` render `<button class="ocr-tab active">Công cụ OCR`, không có `onClick`, không có biến tab; tab thứ hai chỉ là breadcrumb của folder. Nguồn gốc con số 4 nhiều khả năng ở shell WinForms (`MainForm.Designer.cs` khai báo 7 `TabPage`, thêm 3, và mảng dashboard có 4 mục mà mục thứ 4 bị comment). ⇒ Thiết kế dưới đây **tự chốt 4 tab**, lấy đúng quy trình xử lý của autoai (upload → inspect → OCR → PDF chuẩn → đẩy vào kho) làm xương sống.
2. **Không tự viết PDF viewer.** Mọi khung "đọc PDF" dùng trình PDF gốc của Chromium/PDFium (`<iframe src=blob:…>`), đúng như tab Quét đang làm.
3. **Engine OCR vẫn là Tesseract** — cùng binary với pipeline (`C:\Program Files\Tesseract-OCR\tesseract.exe`, lang `vie+eng`) nhưng **đường code riêng** `src/common/scanocr` (xem giả định 7): pipeline gọi `tesseract … stdout`, tab Quét gọi `tesseract … tsv` rồi tự viết PDF. Không đưa Python service vào monorepo Go.
4. ~~**PDF ra là PDF 1.7 có lớp chữ ẩn + font nhúng (Identity-H + ToUnicode)**, KHÔNG cam kết PDF/A-2b ở MVP.~~ **ĐÃ BỎ (xem khối CHỐT LẠI)**: PDF ra không có lớp chữ ẩn — nguồn PDF giữ nguyên tệp gốc, nguồn ảnh bọc một trang. Không còn font nhúng, không còn ToUnicode, không cần Ghostscript/PDF/A vì chẳng còn gì để hậu xử lý.
5. Ảnh/PDF đầu vào là **chữ in** (ảnh chụp bảng trắng, scan, PDF tạo từ ảnh). Chữ viết tay không nằm trong phạm vi — đã có đường VLM riêng.
6. Người dùng đơn nhiệm một máy chủ local/dev; thiết kế vẫn chạy được nhiều worker nhờ hàng đợi Postgres (pattern `ClaimExtractPending`).
7. **TÁCH HẲN hai thứ đang cùng tên "OCR" — mục tiêu khác nhau nên code khác nhau.** (a) *OCR nạp kho* (`src/common/extractor/ocr.go`, `OCRImage`): mục tiêu bóc **text thô từng trang** cho pipeline extract/index để search; chỉ cần chuỗi, PSM 6, sai sót chấp nhận được vì còn chunker + agent + `list_unreadable_pages` bù; chạy trong worker ingest, không box, không confidence, không PDF. (b) *OCR tab Quét* (đợt này): mục tiêu trả về **tệp PDF** kèm chữ OCR xem/sửa được, nên vẫn cần word box + confidence để tab Xem & sửa tô sáng và OCR lại theo vùng (lớp chữ vô hình + font nhúng trong bản thiết kế đầu **đã bỏ** — xem khối CHỐT LẠI). Hai thứ đó đi ngược nhau ở chỗ: bên (a) tối ưu "rẻ, ít bước, đủ để tìm"; bên (b) tối ưu "đúng toạ độ, đúng ký tự, nhìn thấy được".
   ⇒ Code của (b) nằm trong **package riêng `src/common/scanocr`** (engine Tesseract TSV + renderer + inspector), **không sửa `src/common/extractor`**, không dùng `OCRConfig` của pipeline, không đổi `pipeline.go`/`pdf.go`. Config cũng tách: section mới `scan_ocr.*` (binary/lang/psm/dpi/ngưỡng/TTL/song song/chất lượng ảnh), pipeline vẫn đọc `ocr.*`. ~~Endpoint preview đồng bộ `/scan/ocr` hiện có giữ nguyên~~ → **endpoint đó đã xoá** cùng package `searchpdf` (xem khối CHỐT LẠI); chỉ còn `/scan/jobs` (async, có box/conf) và hai bảng (`ocr_jobs`, `ocr_job_pages`) không đụng `article_pages`.
   Cái duy nhất hai bên trùng là **đường dẫn binary Tesseract + renderer poppler trên máy** — chia sẻ bằng config, không chia sẻ bằng code. Giá phải trả là ~150 dòng `os/exec` wrapper gần giống nhau; chấp nhận có chủ ý, vì gộp lại thì mỗi lần đổi PSM/ngưỡng cho pipeline là âm thầm đổi luôn chất lượng PDF của người dùng (và ngược lại: thêm TSV/font/PDF/A vào `OCRImage` là kéo nợ vào đường nạp kho đang chạy ổn định).

---

## A. THIẾT KẾ UX/UI

### A.1. Cấu trúc 4 tab

| # | Tab | Vai | Có gì |
|---|---|---|---|
| 1 | **Tài liệu** (`scan-docs`) | đưa file vào + chọn phạm vi OCR | hàng chờ kéo-thả, thẻ từng tệp, kết quả dò text layer, nút "Bắt đầu OCR", chọn trang / vùng |
| 2 | **Xem & sửa** (`scan-review`) | đối chiếu gốc ↔ chữ OCR, bôi vùng để OCR lại, sửa text sai | 2 khung: khung trái = ảnh render từng trang (vẽ vùng được), khung phải = chữ OCR sửa được; thanh từ tự tin thấp |
| 3 | **PDF chuẩn** (`scan-output`) | tệp PDF kết quả để xem/tải (KHÔNG có lớp chữ ẩn) | `<iframe>` PDF gốc của Chromium + nút Tải / mở tab mới / toàn màn hình; "Đẩy vào kho" là V2, chưa làm |
| 4 | **Lịch sử** (`scan-history`) | mọi phiên OCR đã chạy, mở lại không OCR lại | danh sách job (trạng thái, số trang, thời gian), mở lại, xoá, TTL |

Điều hướng: `mat-tab-group` ở đầu trang Quét (thay cho màn hình đơn hiện tại), mỗi tab là một component standalone, state job đặt trong `ScanJobStore` (signals) để chuyển tab không mất việc đang chạy.

**Lý do tách 4 tab** — autoai nhét tất cả vào một màn (sidebar + 2 khung + 3 modal) nên nút nào cũng chen, và vì không có trạng thái "đang OCR từng trang" thật nên người dùng không biết đang ở bước nào. Tách theo **bước nghiệp vụ** (đưa vào → kiểm tra → xuất → quay lại) giúp mỗi màn chỉ có một hành động chính. **Phương án thay thế**: ở lại 1 tab, thêm stepper dọc — bị loại vì vùng chọn OCR cần diện tích ngang (2 khung cạnh nhau).

### A.2. Luồng người dùng (happy path)

```
Kéo 1 PDF scan (200 trang) vào tab Tài liệu
  → BE: lưu blob tạm + dò text layer → thẻ file hiện "200 trang · dạng ảnh · cần OCR toàn bộ"
  → Chọn phạm vi: Toàn bộ (mặc định) | Các trang (nhập "1-5,8,10-12") | Vùng (chọn ở tab 2)
  → Bấm "Bắt đầu OCR" → job chạy ở worker, tab 1 hiện tiến độ THẬT "OCR trang 37/200 · 2 trang lỗi"
  → sang tab Xem & sửa: khung trái trang 1, khung phải chữ; tự tin thấp sáng amber
  → Bôi vùng đọc sai → "OCR lại vùng này" → chỉ vùng đó đổi, còn lại giữ nguyên
  → Bấm "Tạo PDF chuẩn" → tab PDF chuẩn: mở bằng viewer Chromium, tải về hoặc in (không có Ctrl+F vì PDF không nhúng lớp chữ ẩn)
  → "Đẩy vào kho" → V2, CHƯA làm: hiện chỉ tải tệp về máy
```

### A.3. Wireframe mọi trạng thái (text)

**(1) tab Tài liệu — chưa OCR gì**

```
┌ Tab Quét ─────────────────────────────────────────────────────────┐
│ [Tài liệu] [Xem & sửa] [PDF chuẩn] [Lịch sử]                      │
├───────────────────────────────────────────────────────────────────┤
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │  ⤓  Kéo tệp vào đây, hoặc [Chọn tệp]                        │  │
│  │     Hỗ trợ: PDF (kể cả PDF tạo từ ảnh) · JPG · PNG              │  │
│  └─────────────────────────────────────────────────────────────┘  │
│  Ngôn ngữ OCR: [Tiếng Việt (vie+eng) ▾]   Chất lượng: [300 DPI ▾] │
│  ☐ Ép OCR lại dù trang đã có chữ                                  │
└───────────────────────────────────────────────────────────────────┘
```

**(2) tab Tài liệu — đã nạp file, chờ lệnh**

```
│ Tệp đã chọn                                                       │
│ ┌─────────────────────────────────────────────────────────────┐   │
│ │ 📄 quy-dinh-bo-sung.pdf   4,2 MB   200 trang                │   │
│ │    ⊘ Dạng ảnh (scan) — cần OCR toàn bộ     [Xoá]            │   │
│ │    Phạm vi: (•) Toàn bộ  ( ) Trang: [1-5,8___]  ( ) Vùng     │   │
│ │    [▶ Bắt đầu OCR]                                          │   │
│ ├─────────────────────────────────────────────────────────────┤   │
│ │ 📄 hop-dong.jpg   1,1 MB   1 trang                          │   │
│ │    ✓ Đã có chữ sẵn (1 190 ký tự) — không cần OCR            │   │
│ │    [↻ OCR lại dù đã có chữ]              [Xoá]              │   │
│ └─────────────────────────────────────────────────────────────┘   │
```

**(3) tab Tài liệu — đang chạy (tiến độ thật theo trang)**

```
│ ┌─────────────────────────────────────────────────────────────┐   │
│ │ 📄 quy-dinh-bo-sung.pdf    ⏳ Đang OCR trang 37/200  (18%)   │   │
│ │    ▓▓▓▓▓▓▓░░░░░░░░░░░░░░  ~2 phút nữa                       │   │
│ │    ✓ 35 trang xong · ⚠ 2 trang nghi ngờ (ảnh mờ) · ✗ 0 lỗi   │   │
│ │    [⏸ Tạm dừng] [✕ Huỷ]  [→ Xem & sửa trang đã xong]        │   │
│ └─────────────────────────────────────────────────────────────┘   │
```

`(18%)` = `done_pages / total_pages` — **không** mô phỏng bằng timer. autoai tăng +4%/350ms và tự bịa "Đang OCR trang 1/2…" (`OcrTools.tsx:266-280`); đây là lỗi phải tránh.

**(4) tab Xem & sửa — chưa có kết quả**

```
│ [Tài liệu] [Xem & sửa ●] [PDF chuẩn] [Lịch sử]                    │
│ ┌ kiri (55%) ──────────────┬─┬ phải (45%) ───────────────────┐    │
│ │  ◀ 1/200 ▶   [⬚ Vùng]   │ │  Trang 1 — chưa OCR            │    │
│ │  ┌────────────────────┐ │ │                                │    │
│ │  │  (ảnh render 300DPI)│ │ │  Bấm "OCR trang này" hoặc     │    │
│ │  │                     │ │ │  chạy OCR ở tab Tài liệu.     │    │
│ │  └────────────────────┘ │ │                                │    │
│ └─────────────────────────┴─┴────────────────────────────────┘    │
```

**(5) tab Xem & sửa — đã OCR, có từ tự tin thấp**

```
│ ┌ trái ────────────────────┬─┬ phải ──────────────────────────┐    │
│ │ ◀ 1/200 ▶  [⬚ Vẽ vùng]   │ │  Page 1  · TC trung bình 91,8  │    │
│ │  ...bôi đen trên ảnh...  │ │  ☐ Chỉ hiện từ nghi ngờ (<70)  │    │
│ │                          │ │  ┌────────────────────────────┐ │    │
│ │                          │ │  │ PHỤ LỤC: CÁC QUY ĐỊNH BỔ  │ │    │
│ │                          │ │  │ SUNG                      │ │    │
│ │                          │ │  │ 1. Nhân viên [khống]↔ được │ │    │  ← "khống" amber, click = sửa
│ │                          │ │  └────────────────────────────┘ │    │
│ │                          │ │  [↻ OCR lại trang] [✓ Hoàn tất] │    │
│ └──────────────────────────┴─┴────────────────────────────────┘    │
```

**(6) tab Xem & sửa — đang vẽ vùng**

```
│ │  Chế độ vùng (R): kéo để chọn · Shift = vuông · Esc = huỷ     │
│ │  ┌───────────────────────────┐                                │
│ │  │ ┌╌╌╌╌╌╌╌╌╌╌╌╌┐            │   vùng 1: 42,8×6,3 cm          │
│ │  │ ┼╌ trang 3 đoạn điều 11 ┼╌│   [↻ OCR lại vùng] [🗑]        │
│ │  │ └╌╌╌╌╌╌╌╌╌╌╌╌┘            │   vùng 2: …                    │
│ │  └───────────────────────────┘                                │
```

**(7) tab Xem & sửa — trước/sau khi OCR lại vùng (diff)**

```
│  Trước                                 Sau                        │
│  1. Nhân vién khong được tiét 16 …  →  1. Nhân viên không được    │
│     (TC 61,5 · 3 từ nghi ngờ)           tiết lộ thông tin …       │
│                                           (TC 93,1 · 0 từ nghi ngờ)│
│  [Giữ bản cũ]  [Dùng bản mới]  [OCR lại vùng]                    │
```

**(8) tab PDF chuẩn — đã xong**

```
│ [Tài liệu] [Xem & sửa] [PDF chuẩn ●] [Lịch sử]                    │
│  quy-dinh-bo-sung.pdf · 200 trang · 5,8 MB                         │
│  nguồn PDF → trả nguyên tệp gốc (không có lớp chữ ẩn)              │
│ ┌──────────────────────────────────────────────────────────────┐   │
│ │        <iframe> — trình PDF gốc của Chromium                 │   │
│ │        (không Ctrl+F được; xem chữ ở tab Xem & sửa)          │   │
│ └──────────────────────────────────────────────────────────────┘   │
│  [⤓ Tải PDF]  [↗ Mở tab mới]  [⛶ Toàn màn hình]                     │
│  ("Đẩy vào kho tri thức" — V2, chưa làm)                           │
```

**(9) tab PDF chuẩn — chưa tạo**

```
│  Chưa có PDF chuẩn. 194/200 trang đã OCR (6 trang chờ).            │
│  [Tạo PDF từ 194 trang đã xong]  [OCR nốt 6 trang]                 │
```

**(10) trạng thái lỗi (mọi tab)**

```
│ ┌─────────────────────────────────────────────────────────────┐    │
│ │ ⚠ Không OCR được 3 trang (7, 12, 88)                        │    │
│ │    Lý do: máy chủ chưa có engine OCR tiếng Việt (vie)        │    │
│ │    Mã lỗi: ocr.lang_missing · trace: 4f2c…                  │    │
│ │    [↻ Thử lại 3 trang này]  [Bỏ qua]  [Copy mã lỗi]          │    │
│ └─────────────────────────────────────────────────────────────┘    │
```

Không bao giờ hiện nguyên văn `Http failure response for …: 0 Unknown Error` — bộ map lỗi ở B.9. autoai in thẳng `str(e)` của Python và còn đọc body lỗi bằng `res.text()` nên người dùng thấy cả JSON lẫn traceback (`OcrTools.tsx:288-290`, `api:177-180`).

**(11) Lịch sử**

```
│ ┌───────────────────────────────────────────────────────────┐      │
│ │ 📄 quy-dinh-bo-sung.pdf        ✓ 200/200 · 12 phút         │      │
│ │    tạo lúc 09:41 · PDF 5,8 MB · còn lưu đến 06/11 17:00     │      │
│ │    [Mở lại (không OCR lại)] [Tải PDF] [🗑 Xoá ngay]         │      │
│ ├───────────────────────────────────────────────────────────┤      │
│ │ 📄 bien-ban.jpg  ⚠ 1/1 trang lỗi · engine thiếu            │      │
│ │    [↻ Thử lại] [Xoá]                                       │      │
│ └───────────────────────────────────────────────────────────┘      │
```

### A.4. Công cụ chọn vùng (chuột + phím tắt)

Vẽ vùng **trên ảnh render của trang**, không trên `<iframe>` PDF: PDFViewer của Chromium chạy ở process khác và iframe khác origin nên không script được — autoai chứng minh bằng scroll-sync không bao giờ hoạt động (`OcrTools.tsx:204-237` + `MainForm.Ocr.cs:27` navigate-to-string khiến origin là `null`). Khung trái vì thế là `<img>` (blob URL từ backend render trang), khung đọc PDF thuần vẫn là `<iframe>` ở tab 3.

* Kéo chuột trái → rect mới; `Shift` khi kéo → vuông góc; `Alt` khi kéo → bỏ ràng buộc grid.
* Rect đã có: kéo trong = di chuyển, kéo 8 handle = đổi kích thước, `handle` giữa cạnh = resize 1 chiều.
* `Ctrl` + kéo = thêm vùng thứ 2, 3… ; `Backspace`/`Delete` = xoá vùng đang chọn; `Esc` = thoát chế độ vùng; `R` = bật/tắt chế độ vùng; `Ctrl+Enter` = OCR lại toàn bộ vùng đang chọn.
* Snap-to-line: khi thả chuột, nếu vùng cắt ≥ 50% chiều cao một dòng thì nẹp vùng theo **bìa dòng** (để không cắt nửa chữ, Tesseract rất ghét chữ cụt). Tắt bằng `Alt`.
* Toạ độ lưu **chuẩn hoá 0..1** theo kích thước ảnh render ⇒ không phụ thuộc DPI hiển thị, zoom của trình duyệt, hay CSS.

### A.5. Đồng bộ tô sáng giữa hai khung

Hai chiều, mặc định bật:

* Click/`hover` một **dòng** trong khung chữ → khung ảnh vẽ overlay rect của dòng đó (opacity 0,25, viền 2px). Đây là hướng đáng tin nhất vì dòng có bbox.
* Click trên ảnh → xác định dòng có bbox chứa điểm click → scroll + highlight dòng trong khung chữ.
* **Không** tô sáng từ PDF viewer (bất khả thi, như trên).
* `Sync` dùng `line_id` chứ không dùng `% chiều dài document` — autoai sync theo tỉ lệ scroll nên ảnh 1 trang ↔ PDF nhiều trang là lệch ngay.

### A.6. Component Angular Material dùng

`MatTabs` (4 tab), `MatProgressBar` + `MatChips` (tiến độ/lỗi theo trang), `CdkDragDrop` (hàng chờ tệp), `MatFormField`/`MatSelect` (ngôn ngữ, DPI, PSM), `MatRadio`/`MatSlideToggle` (phạm vi OCR), `CdkScrollable` + overlay SVG tự vẽ cho vùng, `MatDialog` (xác nhận xoá, đẩy vào kho), `MatTooltip` cho từ tự tin thấp, `MatSnackBar` cho thông báo nhanh, `MatIcon` (`document_scanner`, `draw`, `picture_as_pdf`, `history`, `rule`).
**Lý do:** đã là stack của repo (`knowledge_ingestion_fe` Angular 21 + Material 21), không thêm dependency mới. **Thay thế:** `ngx-long-text-selection`/`fabric.js` cho vùng — bị loại vì chỉ cần rect, tự vẽ SVG + pointer events là đủ và không phình bundle (bundle budget repo đang chặn).

### A.7. Hiển thị độ tự tin

Mỗi từ có `c` (0..100). Nấc: ≥90 không đánh dấu · 70–89 gạch chân chấm bi nhạt · <70 nền amber + `matTooltip="Tự tin 61% — bấm để sửa"`. Đầu trang hiện `TC trung bình 91,8 · 8 từ <70`.
Bằng chứng thực tế đo hôm nay (trang 1 của `docs_test_case_and_data/Tài Liệu Mẫu/quy_dinh_bo_sung.pdf`, 300 DPI, `-l vie+eng --psm 6`): 387 từ, TC trung bình **91,8**, đúng **8 từ <70**, OCR mất **2,5 s**. ⇒ bộ lọc "<70" chỉ trỏ ~2% số từ, không nhiễu.
**Lý do không dùng màu đỏ cho <50:** dễ lẫn với lỗi hệ thống; amber = "người nên xem", đỏ để dành cho "việc không chạy được".

---

## B. THIẾT KẾ KỸ THUẬT

### B.1. Kiến trúc & dòng dữ liệu

```
FE (tab Quét, 4 tab)
 ├─ POST /scan/jobs            multipart tệp  ─┐
 ├─ GET  /scan/jobs/:id        tiến độ          │  API (cmd/apis, gin)
 ├─ GET  /scan/jobs/:id/pages/:n  words+bbox    │   └ ScanJobService
 ├─ POST /scan/jobs/:id/ocr    scope/range/region  │        │ ghi DB + blob
 ├─ PATCH /scan/jobs/:id/pages/:n  sửa text     │        ▼
 ├─ POST /scan/jobs/:id/render tạo PDF          │   Postgres: ocr_jobs / ocr_job_pages
 ├─ GET  /scan/jobs/:id/download  presigned GET │   SeaweedFS: scan/<job>/source.pdf, pages/…, output.pdf
 └─ POST /scan/jobs/:id/push   vào kho          │
                                                ▼
Worker (cmd/worker) — vòng claim SKIP LOCKED (pattern ClaimExtractPending):
   với mỗi job processing: mỗi trang pending →
     render trang (pdftoppm) → Tesseract TSV → parse word/line/conf → ghi JSONB
   rồi chốt output: nguồn PDF → trỏ thẳng SourceKey; nguồn ảnh → bọc 1 ảnh vào PDF một trang (fpdf) → upload → presigned URL
```

Ba nguyên tắc:

1. **API không OCR.** Bài học autoai: `/process` chạy OCR ngay trong request, `waitress threads=600` nhưng `ocrmypdf.ocr` nắm lock toàn cục + code hijack fd 2 của process (`ocr_tools.py:170-207`, `api.py:598-603`) → N request OCR tuần tự hoá và chen lẫn stderr. Ở đây OCR nằm trong worker, request HTTP chỉ đọc/ghi DB.
2. **RAM theo trang, không theo tệp.** Mỗi trang một vòng đời (render → OCR → ghi → giải phóng), cùng triết lý `article_pages` + cửa sổ trang của pipeline index. Tệp 500 trang không kéo hết lên RAM.
3. **Kết quả trung gian là dữ liệu, không phải text suông.** Lưu **từ + box + confidence**, không chỉ chuỗi — điều autoai không có (không sidecar, không hocr, không `get_text("words")`), nên autoai không thể làm UI sửa theo từ/vùng.
4. **Đường OCR này tách hẳn đường OCR nạp kho** (giả định 7): mọi code mới nằm trong `src/common/scanocr` (cổng engine + adapter Tesseract + inspector PDF). `src/common/extractor/ocr.go`, `pipeline.go`, `article_pages` **không đổi một dòng nào** — pipeline không nhận box/conf, tab Quét không gọi `extractor.OCRImage`. Hai bên chỉ trùng đường dẫn binary trong config, không trùng code. (Package `src/common/searchpdf` trong bản thiết kế **đã bị xoá** — xem khối CHỐT LẠI.)

### B.2. Engine OCR cho **chữ in**: chọn qua cổng, không khóa vào một engine

Đối tượng của tính năng này là **chữ in** (scan, ảnh chụp màn chiếu, PDF xuất từ Word), không phải chữ viết tay. Mà chữ in là bài toán đã giải quyết quá tốt rồi — **rất nhiều engine cùng làm được**, nên thiết kế KHÔNG được cột chặt vào một engine nào: hôm nay Tesseract, ngày mai khách cần tiếng Việt đẹp hơn thì đổi engine mà UI/API/PDF không được phép đổi. Vì vậy engine nằm sau một cổng:

```go
// src/common/scanocr/engine.go — CHỈ tính năng tab Quét dùng; extractor (OCR nạp
// kho) không nhập port này và ngược lại (giả định 7).
type Page struct {
    Width, Height float64 // px của ảnh đã OCR — để chuẩn hoá box
    Lines         []Line  // Line có Text, Box (px), Conf, Words []Word
    MeanConf      float64
}

type Engine interface {
    Name() string
    // Recognize đọc chữ in trên MỘT ảnh (PNG/JPG bytes) và trả dòng+từ kèm
    // toạ độ và confidence — đây là hợp đồng tối thiểu UI bắt buộc phải có.
    Recognize(ctx context.Context, img ImageInfo, data []byte) (*Page, error)
    // Available báo engine có chạy được trên máy này không (binary/cổng HTTP/
    // model đã tải), để UI nói "engine chưa sẵn sàng" thay vì để từng trang fail.
    Available(ctx context.Context) error
}
```

Registry theo đúng khuôn `src/common/extractor/readers.go` (mỗi engine một file, khai báo trong `engines.go`), engine chọn bằng **config `scan_ocr.engine`** (default `tesseract`); adapter mới = thêm 1 file + 1 dòng registry, không sửa service/UI/PDF. **Lý do đặt chuẩn hoá box ở tầng service, không ở engine:** engine nào cũng có đơn vị toạ độ riêng (px, 0..1, toạ độ PDF) — ép tất cả về px của ảnh đã OCR rồi mới chia là cách duy nhất để so chất lượng hai engine trên cùng một trang.

So sánh các engine đang có thật cho **chữ in tiếng Việt**:

| Engine | Chữ in VN có dấu | Word/line box + conf | Chạy ở đâu / tích hợp Go | Chi phí & giấy phép | Ghi chú rủi ro |
|---|---|---|---|---|---|
| **Tesseract 5** (LSTM) | Tốt với `-l vie+eng`; heading in hoa hay mất dấu (đã thấy "PHU LUC" trên fixture) | **Có sẵn**, TSV `level,left,top,width,height,conf` | binary CLI qua `os/exec` — **đã cài, đã cấu hình** (`configs/config.json`) | free, Apache-2.0 | chất lượng phụ thuộc `--psm`; ảnh lệch/2 cột cần 3 chứ không 6 |
| **PaddleOCR** (PP-OCRv4/v5) | Rất tốt, cả layout/bảng | Có (det + rec từng từ) | Python service hoặc ONNX — **thêm process + ~700MB model** | free, Apache-2.0 | đắt nhất về triển khai; CPU yếu là chậm |
| **RapidOCR** (ONNX của PP-OCR) | Như Paddle | Có | chạy được near-headless, vẫn cần runtime model | free, Apache-2.0 | nhẹ hơn Paddle nhưng vẫn là "một service mới" |
| **docTR** | Tốt | Có (line/word) | Python (TF/PyTorch) sidecar | free, Apache-2.0 | cùng vấn đề triển khai như Paddle |
| **VietOCR** | **Tốt nhất cho tiếng Việt** (huấn luyện riêng cho VN, kể cả ảnh xấu) | Có (dùng thêm detector) | Python + torch (~2GB) | free, Apache-2.0 (kiểm lại theo model tải về) | nặng, pin version thủ công |
| **Surya / GOT-OCR2.0 / Deep-OCR (VLM OCR)** | Rất tốt, hiểu layout | Có, kiểu box khác nhau | cần GPU hoặc dịch vụ model | giấy phép theo model — **phải kiểm tra từng bản** | quá tốn cho một tab OCR tài liệu nội bộ |
| **OCRmyPDF** (autoai dùng) | = Tesseract (b nó gọi Tesseract) | **Không** (trừ khi tự lấy `--sidecar`/hocr) | Python pipeline, lock toàn cục | free, MPL-2.0 | autoai là bằng chứng: sai tham số `tessedit_pageseg_mode` nên PSM **chưa từng được áp dụng** (`ocr_tools.py:201` vs `ocrmypdf/api.py:480 tesseract_pagesegmode`, kwargs lạ bị bỏ âm thầm ở `api.py:414-418`) |
| **Cloud: Azure Document Intelligence / Google Document AI / AWS Textract / Mistral OCR** | Tốt | Có | Bearer/SDK, ~1 call/trang | trả tiền/trang | **loại vì bảo mật**: tài liệu nội bộ/ hợp đồng không được rời máy chủ (giả định của brief) |

**Chốt: MVP/V1 = Tesseract adapter**, vì (a) đã có binary + tessdata `vie` trên máy này và code Go `os/exec` đã có khuôn, (b) TSV cho đúng thứ UI cần (word + box + conf) mà không phải viết thêm model nào, (c) đo thật: 2,5 s/trang A4 @300DPI, 387 từ, conf TB 91,8 — đủ cho job 200 trang (~8 phút một luồng, ~2 phút với 4 luồng). **Không gửi ảnh đi đâu cả** (cloud bị loại từ vòng gửi xe vì dữ liệu nhạy cảm).
**V2 khi cần tiếng Việt đẹp hơn:** thêm adapter `vietocr` hoặc `paddle` — chỉ cần nó trả `Page{Lines/Words, Box px, Conf}`; UI (highlight, sửa theo dòng, OCR vùng) và bảng `ocr_job_page_words` giữ nguyên, vì tất cả chỉ nhìn hợp đồng đó. (Bước dựng PDF có lớp chữ ẩn đã bị bỏ khỏi phạm vi — xem khối CHỐT LẠI.)

### B.3. Trích box + confidence — Tesseract TSV (adapter đầu tiên)

Lệnh của adapter Tesseract — **code nằm ở `src/common/scanocr/engine_tesseract.go`, KHÔNG đụng `src/common/extractor/ocr.go`** (đường OCR cho pipeline nạp kho giữ nguyên: `stdout`, PSM 6, không box):

```
tesseract <page.png> stdout -l vie+eng --psm 6 tsv
```

TSV có `level, page_num, block_num, par_num, line_num, word_num, left, top, width, height, conf, text`. `level 5` = từ, `level 4` = dòng. Parse thành:

```json
{
  "page": 1, "dpi": 300, "width_px": 2480, "height_px": 3508,
  "mean_conf": 91.8, "low_conf_words": 8,
  "lines": [
    {
      "id": "b1p2l3", "block": 1, "para": 2, "line": 3,
      "bbox": { "x": 0.2794, "y": 0.0456, "w": 0.1532, "h": 0.0174 },
      "text": "Phiên bản: 2024-Q4",
      "conf": 94.0,
      "words": [
        { "t": "Phiên", "bbox": { "x": 0.2794, "y": 0.0456, "w": 0.0484, "h": 0.0174 }, "c": 96.1 },
        { "t": "bản:", "bbox": { "x": 0.3314, "y": 0.0456, "w": 0.0403, "h": 0.0174 }, "c": 91.8 }
      ]
    }
  ]
}
```

`bbox` **chuẩn hoá 0..1** (chia cho `width_px`/`height_px`) — đây là đơn vị duy nhất FE và renderer cùng dùng; px và pt chỉ tồn tại trong lúc tính.
`conf == -1` (dòng header/không phải chữ) bị loại từ lúc parse.

### B.4. API spec (gin, DTO theo convention repo)

Tất cả dưới `authed` (`/api/v1`), envelope `dtos.Result[T]`, response `snake_case`.

| Method + path |_body_ | 200 trả | Ghi chú |
|---|---|---|---|
| `POST /scan/jobs` | multipart `file`, form `lang`, `dpi`, `psm`, `force_ocr` | `{id, name, page_count, has_text_layer, ocr_needed_pages, status, upload_expires_at}` | lưu blob + dò text layer **một lần** ngay tại upload |
| `POST /scan/jobs` | JSON `{object_key, name}` (tệp đã có trong kho, ví dụ từ article) | như trên | đường "OCR lại bài đang có" |
| `GET /scan/jobs?limit&status` | – | `{items:[…], total}` | tab Lịch sử |
| `GET /scan/jobs/:id` | – | `{id,status,total_pages,done_pages,failed_pages,low_conf_pages,progress,eta_seconds,error?,output_url?}` | **nguồn tiến độ duy nhất** (FE poll) |
| `POST /scan/jobs/:id/ocr` | `{scope:"all"\|"pages"\|"region", pages:[1,2,5], region?:{page,x,y,w,h}}` | `{queued_pages, job_status}` | `force=true` mới nhận `scope:"all"` khi đã OCR xong |
| `GET /scan/jobs/:id/pages/:num` | – | DTO B.3 + `{render_url, render_size_px, rotation, edited:bool}` | `render_url` = presigned GET ảnh đã render, TTL 5 phút |
| `PATCH /scan/jobs/:id/pages/:num` | `{lines:[{id,text}]}` | `{updated_lines, mean_conf}` | sửa tay; server tính lại layout từ theo `GetStringWidth` |
| `POST /scan/jobs/:id/render` | – | `{status:"rendering", output_url?}` | dựng PDF |
| `GET /scan/jobs/:id/download` | – | `{url, expires_at, size_bytes, words}` | presigned GET, không proxy qua API |
| `POST /scan/jobs/:id/push` | `{category_id, title?, tags?}` | `{article_id, extract_status, index_status}` | copy blob → tạo `article` → hàng đợi extract/index có sẵn |
| `POST /scan/jobs/:id/cancel` | – | `{status}` | cancel = context; trang đang OCR bị cắt |
| `DELETE /scan/jobs/:id` | – | `{deleted, objects_removed}` | xoá cứng blob + row |

**Tiến độ: polling, không SSE/WebSocket.** Lý do thật dụng: API xác thực bằng `Authorization: Bearer` (header) mà `EventSource` không gửi header được; muốn SSE phải thêm token một lần trong query string — thêm đường tấn công cho một tính năng chỉ cần độ trễ 1 giây. FE poll `GET /scan/jobs/:id` với backoff 1s → 2s → 3s (dừng khi `status ∈ {done, failed, cancelled}`). **Thay thế (V2):** SSE sau khi Auth P1 xong (cookie HttpOnly thì `EventSource` dùng được ngay).

**Tự động dò text layer (điểm autoai làm sai).** autoai dùng `_has_text()` = "nếu *bất kỳ* trang nào có *ký tự nào đó* thì coi như đã có chữ" (`ocr_tools.py:124-136`) → PDF lai (1 trang số + 199 trang scan) **không OCR trang nào**, và PDF hỏng/mật mã bị xếp là "scan" rồi đưa vào OCR. Ở đây đo **theo từng trang**:

```
per_page_chars[i] = rune count text layer trang i          (pdftotext theo trang)
needs_ocr[i]      = per_page_chars[i] < threshold_per_page  (mặc định 40 rune)
                     || force_ocr
has_text_layer    = số trang needs_ocr == 0
rotation[i]       = từ pdfinfo (xử lý trang xoay 90/180/270)
```

Ngưỡng 40 rune lấy đúng ý tưởng `OCRConfig.MinTextChars` đã có trong repo (mặc định 0 = tắt cho pipeline, nhưng tab Quét là do người dùng chủ động yêu cầu nên bật ngưỡng). Kết quả hiển thị cho người dùng: "198/200 trang là ảnh — sẽ OCR 198 trang, giữ nguyên 2 trang đã có chữ".

### B.5. Lớp chữ vô hình — đã kiểm chứng bằng spike Go

**Spike đã chạy hôm nay** (`.tmp_spike/main.go`, xoá sau khi chốt): Tesseract TSV của trang thật → PDF một trang A4 bằng `github.com/go-pdf/fpdf v0.9.0` (đã có trong `go.mod`).

Kết quả đo được:

* PDF: 1 trang, **595,2 × 841,9 pt** (A4), `Producer: FPDF 1.7`, không mã hoá, 437 KB.
* Content stream: **387 toán tử `Tj`, 100% nằm sau `3 Tr`** (render mode 3 = vô hình). Không có mode `0 Tr` nào sót.
* `pdftotext -enc UTF-8` trích ra đúng thứ tự đọc, **có dấu tiếng Việt và có khoảng trắng giữa từ** ("1. Làm thêm giờ ngày thường được tính 150% lương cơ bản").
* Render PDF ra ảnh rồi so với ảnh gốc: mean diff **2,38/255**, số điểm ảnh lệch >40 chỉ **0,92%** (và nằm ở viền chữ do downsample 300→100 DPI) ⇒ **không thấy chữ in ra**.
* Font: `AddUTF8FontFromBytes` với `roboto-regular.ttf` (53 KB, có dấu tiếng Việt — chữ "lương", "Phụ" trích đúng); file font này **đã xoá khỏi repo** cùng spike (sản phẩm không nhúng font — xem khối CHỐT LẠI).

Phép biến đổi toạ độ (px 300DPI, góc trên-trái → pt PDF, góc dưới-trái):

```
pt = px * 72 / dpi
x_pt   = bbox.x
baseline_pt = (bbox.y + bbox.h * 0.82) * 72 / dpi     // 0,82h ≈ chân chữ La-tinh
size_pt = bbox.h * 72 / dpi * 0.78                     // 0,78 ≈ ascender+descender của cap-height
if size_pt < 3 → 3
```

Trang xoay (`/Rotate 90`) thì nhân toạ độ bằng ma trận quay của fpdf (`TransformRotate` + `TransformScale`) theo góc của trang, **không** OCR ảnh đã xoay lệch khung.

**Vì sao tự dựng bằng fpdf thay vì gọi OCRmyPDF:** (1) ta đã có word box + conf từ TSV vì cần chúng cho UI, phần "khó" nhất của OCRmyPDF mình làm rồi; (2) không Python service; (3) sửa text tay xong thì dựng lại lớp chữ từ **chính text đã sửa** là bắt buộc, library đóng gói sẵn không cho chèn "text đã hiệu đính" vào đúng box; (4) điều khiển được số byte (chọn JPEG quality cho trang, mục B.8).
**Rủi ro còn lại + cách chặn:** khoảng trắng giữa từ do PDFium suy ra từ khe hở — nếu hai từ sát nhau, Ctrl+F vẫn ra nhưng copy có thể dính. Biện pháp trong code: sau mỗi từ, **không** tự thêm space; để renderer tính `advance = GetStringWidth(word)` rồi đặt từ kế tiếp tại `x_prev + max(w_word, w_glyph)` — khi cần, chèn `TJ` với offset âm/dương đúng bằng khe hở thực của box. Kiểm chứng bằng test E2E copy (mục C.3), không đoán.

### B.6. OCR lại một vùng — toạ độ và chính sách thay thế

1. FE gửi `region {page,x,y,w,h}` **chuẩn hoá**. Server quy về px theo ảnh đã render: `px = ratio * width_px`.
2. Cắt ảnh bằng `pdftoppm -x -y -W -H` (hoặc cắt trong RAM bằng `image/png` rồi ghi PNG tạm) → chạy Tesseract trên **ảnh cắt**.
3. Box trả về tính theo gốc của ảnh cắt ⇒ **cộng offset** `x0,y0` rồi chia lại kích thước trang để về chuẩn hoá. Đây là chỗ dễ sai nhất: nếu quên offset thì chữ vô hình nhảy sang vị trí khác, PDF trông vẫn ổn nhưng Ctrl+F ra đúng chỗ sai.
4. **Chính sách thay thế: snap-to-line (mặc định).** Mọi dòng *cũ* có giao vùng ≥ 50% chiều cao dòng bị loại khỏi `lines`; các dòng *mới* từ vùng được chèn vào và sắp lại theo `(y, x)`. Chọn mức này vì OCR một phần dòng thường sinh ra fragment (một nửa từ), ghép chồng lên dòng cũ sẽ nhân đôi chữ trong lớp vô hình.
   *Thay thế:* (a) *chồng lấp* — giữ dòng cũ rồi đặt chữ mới đè: bị loại vì Ctrl+F ra kết quả trùng, người dùng tưởng OCR 2 lần; (b) *xoá theo pixel* — xoá mọi từ có box nằm trong vùng: hợp khi chỉ sửa vài từ nhưng dễ bỏ sót từ cao (`h`, `g`, dấu hỏi tiếng Việt), nên chỉ là tuỳ chọn `granularity:"word"`.
5. Region **không** xoá bằng chứng cũ: giữ `lines` cũ trong `ocr_job_page_lines.superseded = true` để tab Xem & sửa hiển thị diff "trước/sau" (wireframe 7) và cho phép "Giữ bản cũ".

### B.7. Dựng tệp đầu ra (đã đơn giản hoá — không còn lớp chữ ẩn)

`buildOutput(ctx, job)` chạy ở worker, **một** bước:

```
ext := đuôi(job.SourceName)
if ext == ".pdf":  repo.MarkDone(job.ID, job.SourceKey)   // output = chính tệp gốc
else:              tải blob nguồn → imagePagePDF(path, ext, RenderDPI) → Upload(scanOutputKey) → MarkDone
```

```go
// imagePagePDF: khổ trang suy từ pixel ảnh + DPI nên không kéo giãn
wPx, hPx := measureImage(path)          // DecodeConfig — không giải mã toàn ảnh
wPt, hPt := float64(wPx)*72/dpi, float64(hPx)*72/dpi
pdf := fpdf.NewCustom(&fpdf.InitType{UnitStr: "pt"})
pdf.SetMargins(0,0,0); pdf.SetAutoPageBreak(false,0)
pdf.AddPageFormat("P", fpdf.SizeType{Wd: wPt, Ht: hPt})
pdf.RegisterImageOptions(path, opts)     // opts.ImageType = "JPG" | "PNG"
pdf.ImageOptions(path, 0, 0, wPt, hPt, false, opts, 0, "")
```

* **RAM là hằng số**: không giữ danh sách trang, không buffer ảnh theo trang, không font nhúng. Job 300 trang nguồn PDF không render một trang nào (chỉ `MarkDone` trỏ `SourceKey`), nên `OutputKey == SourceKey` là trạng thái **hợp lệ** — `removeObjects` dedupe khi xoá/janitor.
* `RegisterImageOptions` đọc tệp và cache vào `f.images`, `ImageOptions` dùng lại entry đó ⇒ ảnh chỉ đọc **một lần**, không giải mã hai lần.
* fpdf chỉ nhúng được **JPEG/PNG** ⇒ đây là lý do `scanExts` **không nhận WEBP**. Ảnh không đo được khổ (`ErrFormat`) rơi về A4@150 (1240×1754 px).
* ~~Font nhúng + `SetTextRenderingMode(3)` + PDF/A~~ — **không còn áp dụng**: bỏ lớp chữ ẩn thì không có gì để vẽ vô hình, cũng không còn PDF/A để hậu xử lý bằng Ghostscript.

### B.8. Kích thước, hiệu năng, hàng đợi

| Vấn đề | Quyết định | Con số/bằng chứng |
|---|---|---|
| OCR chậm | hàng đợi Postgres + N worker, claim theo trang (không theo job) | Tesseract 2,5 s/trang ⇒ 200 trang ≈ 8,3 phút một luồng, ≈ 2,1 phút với 4 luồng |
| Parallelism | semaphore `worker.ocr_max_parallel` mặc định **2**, mỗi job ≤ 2 trang đồng thời | OCRmyPDF của autoai bị lock hoá thành tuần tự; Tesseract tiến trình độc lập nên song song được, nhưng mỗi trang là 1 process + 1 ảnh 2480×3508 (~35 MB RAM) |
| RAM | xử lý theo trang, giải phóng ngay; không giữ cả tệp trong RAM | pattern `article_pages` + `eachPageWindow` |
| Timeout | 60 s/trang (gấp ~24 lần mức đo), job-level deadline 30 phút, `ctx` cancel | `scan_api.go` hiện chặn request 8 phút — job async nên bỏ chặn đó |
| Retry | 3 lần/trang, backoff 5s/15s/45s; lỗi `invalid image` không retry | giống `extract_attempts` + `next_retry_at` |
| Trần upload | 100 MB/tệp (bằng `ScanOCRMaxBytes` hiện có), 500 trang/job, 5 job/user đồng thời | pipeline không giới hạn số trang nhưng tab tương tác thì nên |
| Ảnh trang trong PDF | trang scan **giữ nguyên ảnh gốc của PDF** nếu có (nhúng lại JPEG/PNG gốc bằng `ImageType:"JPEG"`), chỉ render lại khi cần OCR. Chất lượng ảnh = JPEG quality 80 cho output | spike: 1 trang PNG @300DPI = 416 KB, PDF ra 437 KB ⇒ 200 trang ≈ 85 MB. JPEG q80 giảm còn ~1/4–1/6. Đây là lý do KHÔNG nhúng PNGLossless hàng loạt |
| Ảnh nhỏ để UI | render ảnh xem ở 120–150 DPI (`pdftoppm -r 150`), riêng ảnh OCR dùng 300 DPI | vẽ vùng trên ảnh 150 DPI vẫn chính xác vì toạ độ chuẩn hoá |

### B.9. Lỗi thân thiện (map ở FE, không ở BE)

BE trả `errors.NewCustomHttpError(code, <machine_code>, "thông điệp tiếng Việt")`; FE có `ScanErrorMapper` dịch **mọi** `HttpErrorResponse` về tiếng Việt + luôn kèm nút thử lại:

| Tình huống | `code` BE | Câu tiếng Việt hiển thị | Nút |
|---|---|---|---|
| Tệp quá lớn | `scan.too_large` | "Tệp vượt quá 100 MB — hãy tách nhỏ hoặc nén" | Tải tệp khác |
| Sai định dạng | `scan.unsupported` | "chỉ nhận ảnh JPG/PNG hoặc PDF" | — |
| Tệp hỏng/mật mã | `scan.invalid` | "Tệp hỏng hoặc có mật khẩu — mở khoá rồi tải lên lại" | Thử lại |
| OCR chưa bật | `ocr.disabled` | "Máy chủ chưa bật OCR (`ocr.enabled`)" | Thử lại |
| Thiếu `vie` | `ocr.lang_missing` | "Máy chủ chưa có bộ tiếng Việt (`vie`) cho Tesseract" | Thử lại |
| Trang trắng | `ocr.no_text` | "Trang này không có chữ nào (ảnh trắng/mờ)" | OCR lại trang / Bỏ qua |
| Trang lỗi hàng loạt | `ocr.page_failed` | "Không OCR được N trang (7, 12, 88)" | Thử lại N trang |
| Job đang chạy | `job.running` | "Phiên OCR đang chạy — đợi hoặc huỷ trước" | Huỷ |
| 504 | `scan.timeout` | "Máy chủ xử lý quá lâu, đã lưu phần xong — tiếp tục?" | OCR nốt phần còn lại |
| Mất mạng (`status 0`) | – (FE sinh) | "Không kết nối được máy chủ (9001). Kiểm tra API đang chạy rồi thử lại." | Thử lại |
| 401 | – | "Phiên đăng nhập hết hạn — đăng nhập lại, phần OCR vẫn còn nguyên" | Đăng nhập lại |

Kèm `trace_id` để copy khi báo lỗi, **không** hiện traceback/JSON. Interceptor chỉ ghi `message_chars` vào log, không ghi nội dung OCR.

### B.10. Mô hình dữ liệu

```sql
CREATE TABLE ocr_jobs (
  id BIGSERIAL PRIMARY KEY,               -- theo convention BaseModel của repo
  user_id BIGINT NOT NULL,
  source_name TEXT NOT NULL,
  source_key TEXT NOT NULL,               -- scan/<job>/source.<ext>
  output_key TEXT,                        -- scan/<job>/output.pdf
  status TEXT NOT NULL,                   -- pending|running|done|failed|cancelled
  total_pages INT NOT NULL DEFAULT 0,
  done_pages INT NOT NULL DEFAULT 0,
  failed_pages INT NOT NULL DEFAULT 0,
  lang TEXT NOT NULL DEFAULT 'vie+eng',
  dpi INT NOT NULL DEFAULT 300,
  psm INT NOT NULL DEFAULT 6,
  force_ocr BOOLEAN NOT NULL DEFAULT false,
  engine TEXT NOT NULL DEFAULT 'tesseract',
  error_code TEXT, error_message TEXT,
  attempts INT NOT NULL DEFAULT 0,
  next_retry_at TIMESTAMPTZ,              -- lease/backoff, pattern ClaimExtractPending
  expires_at TIMESTAMPTZ NOT NULL,        -- TTL bản tạm
  created_at/updated_at/deleted_at …
);
CREATE TABLE ocr_job_pages (
  job_id BIGINT NOT NULL REFERENCES ocr_jobs(id) ON DELETE CASCADE,
  page_num INT NOT NULL,
  status TEXT NOT NULL,                   -- pending|ocr_done|edited|failed|skipped_has_text
  render_key TEXT, render_dpi INT,
  width_px INT, height_px INT, rotation INT,
  has_text_layer BOOLEAN, chars_existing INT,
  words JSONB,                            -- cấu trúc B.3 (bbox chuẩn hoá + conf)
  mean_conf REAL, low_conf_words INT,
  superseded_lines JSONB,                 -- cho diff trước/sau khi OCR vùng
  text TEXT,                              -- bản cuối để dựng PDF (đã hiệu đính)
  attempts INT, last_error TEXT,
  PRIMARY KEY (job_id, page_num)
);
CREATE INDEX ocr_jobs_claim ON ocr_jobs (status, next_retry_at);
```

Repository `ocr_job_repository.go` theo đúng convention: chỉ làm việc với `domain` model, dùng `dbConn` trong transaction, claim bằng `FOR UPDATE SKIP LOCKED` (`clause.Locking{Strength:"UPDATE", Options:"SKIP LOCKED"}` — copy khuôn `articles_repository.go:424`). **Không** đặt magic number trong repo: trần retry/DPI/ngưỡng rune vào `src/common/constants`.

### B.11. Bảo mật & vòng đời tệp tạm

* **Bucket riêng cho bản OCR tạm** (`scan/…`) ≠ bucket bài viết chính; object ACL private, chỉ truy cập qua presigned GET TTL **5 phút** (`PresignedGetURL` đã có). Không proxy stream qua API (đỡ tốn bandwidth, đỡ leak).
* **TTL 24 giờ** cho job `done`/`failed`, **7 ngày** nếu job đã `push` vào kho (PDF chuẩn đã hoá thành article thì bản tạm không còn giá trị). `scanJanitor` chạy theo pattern `refresh_token_janitor.go`: xoá object + row quá `expires_at`, log số lượng, **không** log tên tệp (tên tệp có thể nhạy cảm — đã có convention ẩn giá trị dài/query).
* Người dùng chỉ thấy/xoá job của mình (`user_id` filter ở mọi query — scope enforcement đã là convention của repo).
* Không gọi bất kỳ API bên ngoài; không gửi nội dung đi đâu (cloud OCR bị loại ở B.2 vì lý do này).
* `push` là **hành động tường minh**: chỉ tạo article khi người dùng bấm, và bài tạo ra đi qua đúng hàng đợi extract/index có sẵn nên không vòng lặp OCR.
* Audit: ghi `logs.Infow("scan: job …", "job_id", …, "pages", …, "chars", …)` — theo convention log, không in nội dung.

---

## C. KẾ HOẠCH TRIỂN KHAI

### C.1. MVP (tab 1 + 3 + 4 chạy được end-to-end)

1. Package mới `src/common/scanocr`: cổng `Engine` + `ImageInfo/Page/Line/Word` (B.2), adapter `engine_tesseract.go` chạy `tesseract … tsv` + parser TSV (hàm thuần, unit test), `inspect.go` (đếm rune text layer từng trang + rotation qua `pdfinfo`), `render.go` (`pdftoppm -r <dpi>` cho 1 trang). **Không sửa `src/common/extractor`.**
2. Config mới `scan_ocr.*` (engine, binary, lang, psm, dpi, threshold, song song, TTL, chất lượng ảnh) + `configs/config.example.json`; `IConfig` getter kiểu `GetScanOCR()`.
3. `domain` + repository `ocr_jobs`, `ocr_job_pages`; migration additive (dev phase được phép).
3. Service `scan_job_service.go`: tạo job, dò text layer theo trang (pdftotext từng trang + `pdfinfo` rotation), ghi blob `scan/<job>/source.<ext>`.
4. Worker loop `ocr_job_service.ClaimPage` trong `cmd/worker`: render → OCR → JSONB; `constants` + semaphore 2.
5. ~~`pdf_text_layer.go`~~ → `scan_job_worker_service.go`: `buildOutput` + `imagePagePDF` (nguồn PDF trỏ thẳng `SourceKey`, nguồn ảnh bọc một trang). Không có font nhúng, không có lớp chữ ẩn.
6. API + routes (`registerScanJobRoutes`): create/list/get/page/retry-page/download/cancel/delete. Thêm annotations + `swag init`.
7. FE tab **Tài liệu** + **PDF chuẩn** + **Lịch sử**: job list, progress poll, iframe blob PDF (dùng lại `ScanPreviewDialog` đã có blob iframe), tải PDF, IndexedDB history hiện tại đổi thành đọc từ BE (chấp nhận giữ cả local cho tệp chưa kịp lên server).
8. `ScanErrorMapper` + nút "Thử lại" ở mọi lỗi.
9. Test: unit parser TSV/coordinate, `TestScanOutputNameAndSourceKey` (tên tệp + key), janitor (`scan_job_janitor_test.go`), E2E qua `apiprobe` (upload → poll → download → mở tệp được).

### C.2. V1 (tab 2 + vùng + sửa tay)

10. Endpoint `GET …/pages/:num` (words+bbox+render_url) và `POST …/ocr` với `scope:"region"`.
11. Worker: cắt ảnh, OCR crop, offset box, chính sách snap-to-line, lưu `superseded_lines`.
12. FE tab **Xem & sửa**: `<img>` + overlay SVG, region tool (mouse + `R/Shift/Esc/Ctrl+Enter/Delete`), snap-to-line, 2 chiều highlight, diff trước/sau, thanh lọc "từ <70", sửa text theo dòng.
13. `PATCH` lines + tính lại width bằng `fpdf.GetStringWidth`; "Tạo lại PDF" sau khi sửa.
14. Highlight đồng bộ theo `line_id`; tự sinh `caption`/số trang cho mỗi khung.

### C.3. V2

15. PDF/A-2b qua Ghostscript (kèm `output_profile`).
16. Engine adapter `vietocr`/`paddle` sau cổng `OCREngine` interface (chỉ đổi wiring trong `loader/ocr.go`).
17. Batch nhiều tệp một lượt, kéo-thả cả thư mục (autoai có `webkitdirectory` nhưng không lọc đuôi → mọi tệp lạ đều 400).
18. SSE tiến độ sau Auth P1; xuất `.txt`/`.docx` (khuôn layout của `api/ocr_tools_api.py:455-611` đáng tham chiếu: gộp dòng theo Δsize ≤1,5 và gap ≤1,2× line height, heading ≥1,25× thân).
19. "OCR lại trang hỏng" nối vào `admin/reset-extract` hiện có để sửa chất lượng bài đã nạp.

### C.4. Rủi ro & giảm thiểu

| Rủi ro | Giảm thiểu |
|---|---|
| Copy/Paste dính chữ (PDFium suy khoảng trắng từ khe hở) | Test E2E copy từng trang fixture; khi cần dùng `TJ` offset thay vì đặt từng `Tj` |
| Mất dấu ở heading in hoa (đã thấy "PHU LUC" trong TSV) | Đây là lỗi **engine**, không phải lớp chữ. Nói rõ trong UI "có N từ nghi ngờ", cho sửa nhanh; V2 đổi engine |
| Ảnh 300 DPI nhúng PNG làm PDF 100+ MB | JPEG q80 cho output, giữ PNG cho OCR tạm; đo `size_bytes` và cảnh báo >50 MB |
| Toạ độ region lệch do quên offset/cắt nửa dòng | Test thuần hàm `offsetRegionBoxes`, snap-to-line mặc định, diff trước/sau để người dùng thấy ngay |
| Đổi DPI làm lệch toàn bộ box đã lưu | Box chuẩn hoá + lưu `render_dpi` theo trang; luôn dựng PDF theo `render_dpi` đã lưu |
| Job treo giữa chừng (worker chết) | lease `next_retry_at` + `ReleaseXxxClaims` khi shutdown (pattern `ReleaseExtractClaims`) |
| Phình kho tạm | janitor TTL + nút Xoá ngay + đếm `size_bytes` trong Lịch sử |
| Trùng với tiền xử lý nạp kho | Tab Quét **không** đụng `article_pages`; chỉ khi bấm "Đẩy vào kho" mới tạo article và đi qua pipeline quen |
| Font thiếu ký tự | Font OFL nhúng (Roboto latin-ext) + test dựng PDF với chuỗi fixture chứa `ặ ỷ ỡ ẹ ỗ`; nếu thiếu → fallback Noto Sans, ghi vào test |

### C.5. Test nghiệm thu (định nghĩa "xong")

> **Cập nhật:** các mục nhắc `Ctrl+F` / bôi đen copy / `POST …/push` **không còn là tiêu chí** — lớp chữ ẩn và "Đẩy vào kho" đã bị bỏ khỏi phạm vi (xem khối CHỐT LẠI). Tiêu chí còn hiệu lực: tiến độ theo trang thật, skip trang đã có chữ, PDF lai, huỷ/xoá sạch object, RAM hằng số, và mở được tệp PDF kết quả.

1. ~~PDF chỉ ảnh 200 trang → mở tab PDF chuẩn, `Ctrl+F` chuỗi "phụ lục" **có kết quả**, số trang khớp.~~ → nay chỉ cần: mở tab PDF chuẩn thấy tệp, số trang khớp.
2. ~~Bôi đen một đoạn rồi Ctrl+C → dán ra … có dấu, có khoảng trắng.~~ → nay kiểm ở tab Xem & sửa (chữ OCR có dấu), không ở PDF.
3. PDF đã có text layer (Word→PDF) → tab Tài liệu báo "đã có chữ, không OCR", job `done` mà **0 trang OCR** (đo bằng `ocr_job_pages.status='skipped_has_text'`).
4. PDF lai 1 trang số + 3 trang scan → **3** trang được OCR (autoai sẽ bỏ cả 4).
5. Vẽ vùng quanh một dòng sai → OCR lại → **chỉ** dòng trong vùng đổi (`lines` ngoài vùng byte-identical trước/sau), diff hiện đúng, Ctrl+F tìm được bản mới.
6. Tắt Tesseract → job fail với `ocr.engine_missing`, UI hiện "Máy chủ chưa có engine OCR" + nút Thử lại, bật lại rồi bấm Thử lại thì job chạy tiếp từ trang chưa xong.
7. Cắt mạng giữa chừng → UI hiện "Không kết nối được máy chủ (9001)…" chứ **không** phải `Http failure response … 0 Unknown Error`; tiến độ phục hồi từ BE sau khi reconnect.
8. Huỷ job giữa chừng → trang đang OCR dừng trong ≤ 60 s, các trang xong vẫn giữ, "Tạo PDF từ phần đã xong" chạy được.
9. `DELETE` job → không còn object nào dưới `scan/<job>/` (kiểm tra bằng `aws s3 ls`/`list_storage_objects`).
10. `POST …/push` → article mới xuất hiện, `extract_status=done` vì đã có text (không OCR lại lần 2).
11. 500 trang, RAM worker không vượt ~500 MB (mỗi trang một vòng đời).
12. `go build && go vet && golangci-lint run && go test ./...` xanh; FE `npm run lint && npm test && npm run build` xanh; `function_budget_test.go` không cần thêm entry mới.

### C.6. Việc đã kiểm chứng để viết tài liệu này

* Spike fpdf (đã chạy, file tạm `.tmp_spike/` **đã xoá** sau khi đo — mã nằm ở B.7): TSV + ảnh 300DPI → `ki_searchable.pdf`; kết quả như B.5 (387 `Tj`, toàn bộ `3 Tr`, A4, `pdftotext` ra tiếng Việt có dấu, ảnh render không thấy chữ). Lệnh đã dùng: `go run ./.tmp_spike <tsv> <png> knowledge_ingestion_fe/public/fonts/roboto-regular.ttf <out.pdf>` (thư mục tạm **và** file font trong `public/fonts/` đều đã xoá, không còn trong repo) — lưu ý API đúng là `fpdf.SizeType{Wd, Ht}`, **không có** `fpdf.Size`.
* Đo OCR thật: `tesseract scan_page-1.png stdout -l vie+eng --psm 6 tsv` → 2,5 s, 387 từ, mean conf 91,8, 8 từ <70.
* Đo đường render ảnh trang để vẽ vùng: `pdftoppm` trong repo là `D:\code\tools\poppler\poppler-24.08.0\Library\bin\pdftoppm.exe` (trỏ trong `configs/config.json:101`), `pdftoppm -r 150 -png -f 1 -l 1 <pdf> <out>` → `*-150-01.png` 1240×1754 px (đúng 150 DPI A4) → toạ độ chuẩn hoá nhân lại là ra px để cắt vùng. Đây là cách tab 2 có ảnh để vẽ vùng mà không đụng PDF viewer.
* Đọc autoai: xem các `file:line` đã dẫn ở A/B — những chỗ "đừng copy" chốt thành defect list: tiến độ bịa, lỗi raw Python/JSON, `_has_text` một trang là bỏ cả file, scroll-sync cross-origin không bao giờ chạy, `.docx` lọt `_PASS_THROUGH` nên không OCR mà vẫn đếm "words" từ file nhị phân, TIFF nhiều trang chỉ lấy trang 1, không TTL/dọn dẹp, `index_started:true` trả về dù indexer ném lỗi.

*(Sau khi code xong đợt này: bổ mục `## Kết quả` rồi chuyển file xuống `plans/archive/2026-10/` và cập nhật `plans/README.md`.)*
