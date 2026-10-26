# Đợt 3 — Detector trang phức tạp (L1)

## Mục tiêu

Biết **trang nào có bố cục phức tạp** (bảng, nhiều cột) ngay trong phase extract, để sau này
mới quyết định có dùng L2 (`pdftohtml -xml`) hay VLM cho đúng những trang đó — thay vì đoán
theo loại file. Không cần Ollama, không gọi mạng, test được offline.

## Vì sao không lưu text `-layout`

Chạy `pdftotext` thêm lần 2 với `-layout` để **đo** bố cục, nhưng **không lưu** bản text đó —
lưu thì dung lượng `article_pages` gấp đôi mà lần sau cần lại chỉ việc chạy lại (tất định,
~ms/trang). Chỉ lưu **điểm số** (1 float) để về sau đổi ngưỡng mà không phải trích lại cả kho.

## Thay đổi

| Nơi | Việc |
|---|---|
| `extractor.Page` | thêm `Layout float64` (điểm 0..1, 0 = tắt/không đo) |
| `extractor.ExtractOption` | tuỳ chọn dạng **variadic** (`WithLayoutDetection`) + type `ExtractOptions` để fx tiêm được — **không** đổi `OCRConfig` thành struct options |
| `extractor/layout.go` (mới) | `DetectLayout(text) LayoutSignals`, `Signals.Score()`, `IsComplex(score)` |
| `extractor/pdf.go` | pass 2 với `-layout` (chạy trước, tuần tự) → map trang→điểm; pass chính không đổi |
| `domain.ArticlePage` | thêm `LayoutScore float64` (`not null;default:0`) |
| `services/article_extract_service.go` | nhận thêm `extractor.ExtractOptions`; trải ra khi gọi extract; ghi `LayoutScore` |
| `config` + `configs/config.json` | `worker.layout_detector` (mặc định **false**) |
| `loader/worker.go` | provide `extractor.ExtractOptions` từ config (tắt → slice rỗng) |

### Vì sao variadic thay vì gộp thành một struct options

Đổi `ExtractRawPages(..., ocr OCRConfig, ...)` thành `(..., opts ExtractOptions, ...)` sẽ bắt sửa ~25 call
site (phần lớn là test) chỉ để thêm một tuỳ chọn mà **mặc định luôn là tắt**. Variadic giữ nguyên hợp
đồng cũ, mở đường thêm tuỳ chọn khác mà không đụng người gọi. `OCRConfig` vẫn giữ nguyên vai "cấu hình
OCR" — không nhồi thêm cờ bố cục vào đó (SRP).

## Cách chấm điểm (công khai, để sau này chỉnh bằng dữ liệu)

Đo trên text `-layout` của 1 trang (poppler canh cột bằng **khoảng trắng**, không vẽ `|`):

| Dấu hiệu | Ngưỡng | Trọng số |
|---|---|---|
| Dòng có khe cột (≥2 khoảng trắng liên tiếp SAU phần thụt lề) | ≥ 3 dòng | 0.4 |
| Dòng có ≥3 token số | ≥ 3 dòng | 0.4 |
| Dòng là đường kẻ bảng (`---`, `===`, `___`, `|`, `+`) | ≥ 2 dòng | 0.2 |

`complex` khi tổng ≥ **0.4** **và** trang có ≥ 5 dòng khác rỗng — tức **một dấu hiệu mạnh là đủ**.
Lý do hạ từ 0.6: bảng biểu phí thật (3 cột) thường chỉ lộ ra qua khe cột (0.4) chứ không có 3 token số
mỗi dòng; để 0.6 thì bỏ sót đúng nhóm trang cần xử lý. Hệ quả đã biết:
- Trang chữ thuần (kể cả có thụt lề đầu đoạn) → 0 (không báo nhầm).
- Bảng không đường kẻ nhưng có cột số hoặc khe cột → 0.4–0.8 → complex.
- Trang chỉ có `-----` phân cách, không số, không khe → 0.2 → không complex (tránh báo nhầm trang mục lục).

## Rủi ro đã chấp nhận

- `pdftotext -layout` chậm hơn ~10-20% cho một lần chạy nữa; chỉ bật khi
  `worker.layout_detector = true`.
- Lỗi ở pass bố cục **không** làm hỏng extract: pass này best-effort, lỗi chỉ log và điểm = 0.
- Ngưỡng là đoán có cơ sở, chưa calibrate — nên lưu `layout_score` chứ không lưu bool, để
  đổi ngưỡng về sau chỉ là đổi hằng số.
- Trang đã trích TRƯỚC tính năng giữ `layout_score = 0`, tức "chưa đo" — muốn có số phải trích lại
  (hoặc chấp nhận chỉ đo trên bài mới). Không backfill vì không thể biết trang cũ có bảng hay không.

## Nghiệm thu

- [x] 6 ca test cho `DetectLayout`: chữ thuần (có thụt lề), bảng 3 cột, trang số liệu, mục lục chấm dẫn, đường kẻ đơn thuần, trang rỗng.
- [x] `TestPDFLayoutScoreWiring`: đo THẬT một PDF bảng 3 cột qua `pdftotext` — bật option thì `IsComplex(Layout)` đúng, không bật thì `Layout == 0` (khoá phần nối dây, không chỉ phần chấm điểm).
- [x] `go build ./...`, `go vet ./...`, `go vet -tags stress ./src/controller/services/`, `golangci-lint run ./...` (0 issues), `go test ./...` xanh.
- [x] Mặc định TẮT (`worker.layout_detector = false`): boot và extract hiện tại không chạy thêm lệnh nào.
- [x] Cột thật trên DB dev: `article_pages.layout_score numeric NOT NULL DEFAULT 0` (AutoMigrate qua test postgres kết nối DB thật).

## Kết quả

- Đã làm: `extractor/layout.go` (+test), `Page.Layout`, variadic `ExtractOption`/`ExtractOptions`,
  lượt đo `-layout` trong `pdf.go`, `ArticlePage.LayoutScore`, `worker.layout_detector`, provider ở
  `loader/worker.go`, service nhận `opts` và ghi điểm, sửa 6 call site của stress test.
- Sai lệch so với kế hoạch ban đầu (đã ghi lại ở trên): dùng **variadic** thay vì gộp struct options
  (vì ~25 call site `OCRConfig`), ngưỡng **0.4** thay vì 0.6 (bảng biểu phí thật chỉ lộ qua khe cột).
- Phát sinh ngoài dự kiến: `extractPDFPages` đổi tham số sang `extractConfig` làm 3 call site trong
  `pdf_test.go` phải sửa — test không bắt được lỗi này (`go build` mới bắt), nhắc lại bài học cũ:
  sau khi đổi chữ ký nội bộ phải chạy `go vet ./...` chứ không chỉ `go build`.
- Chưa làm (cố ý): index cho `layout_score` (chờ L2 đọc theo ngưỡng), backfill trang cũ, và quyết định
  L2/VLM — cái cuối phụ thuộc spike Ollama ngày 27/10 (xem `plans/2026-10-25-dot-2-vlm-reader.md`).

## Commit

1. `docs(rag): add batch 3 plan for complex page detector`
2. `feat(extractor): detect complex page layout from pdftotext -layout`
3. `feat(pipeline): persist layout score on extracted pages`
