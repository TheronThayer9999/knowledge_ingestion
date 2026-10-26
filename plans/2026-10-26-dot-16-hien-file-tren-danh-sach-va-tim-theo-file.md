# Đợt 16 — Hiện định danh file trên danh sách tài liệu + tìm theo file

## Vấn đề

Đợt 15 đã in `file` (= storage key) trên mọi dòng log của worker, nhưng trên UI người
dùng vẫn chỉ thấy `articles.name` — tên do người dùng đặt, sửa được trong dialog upload
và dialog edit. Muốn đối chiếu "dòng log này là tài liệu nào" phải mở dialog chi tiết
(mới có `DOC_DETAIL_SOURCE_FILE`), và ô tìm kiếm danh sách chỉ so `name` nên dán key
từ log vào cũng ra "không tìm thấy".

Đích: nhìn danh sách là biết file nguồn, và dán storage key đọc từ log vào ô tìm kiếm
là ra đúng tài liệu.

## Thay đổi

| File | Việc |
|---|---|
| `core/knowledge/knowledge.models.ts` | Thêm `articleSource(article)` = `storageKey` → `url` → `''` (mirror `fileTag` bên Go) và `matchesArticleQuery(article, term)` — hàm thuần, không phụ thuộc component |
| `pages/documents/documents.ts` | `visible()` lọc bằng `matchesArticleQuery`; expose `articleSource` cho template |
| `pages/documents/documents.html` | Hàng bảng: bọc tên + dòng phụ `.doc-file` (mono, ellipsis, `title` = key đầy đủ) trong 1 cột dọc; card lưới: thêm `.card-file` dưới `.card-meta` |
| `pages/documents/documents.scss` | `.doc-name-wrap` (flex cột, `min-width: 0`), `.doc-file` / `.card-file` = `--text-style-xs` + `--color-muted` + `--font-mono` + ellipsis |
| `shared/i18n/vi.ts`, `en.ts` | Đổi text `DOC_SEARCH_PLACEHOLDER`: "Tìm theo tên hoặc file…" / "Search by name or file…" |
| `core/knowledge/knowledge.models.spec.ts` | Test hàm thuần: `articleSource` (upload → key, link → url, cả hai → key, không có → `''`), `matchesArticleQuery` (khớp tên, khớp key, khớp url, term rỗng = true) |

## Không làm

- Không thêm endpoint/field mới ở BE: `ArticleResponse` đã trả `storage_key` và `url`
  (list dùng cùng struct) nên FE có sẵn dữ liệu.
- Không đổi dialog upload: mỗi dòng đã hiện `row.file.name · size` và tên sửa được.
- Không hiện key ở dialog chat/scope — ngoài phạm vi việc này.

## Kiểm chứng

1. `cd knowledge_ingestion_fe` → `npm run lint`, `npm test`, `npm run build`.
2. `npx prettier --write` cho các file vừa sửa (lint không tự format).
3. E2E thật: chạy API `:9001` + `ng serve`, đăng nhập tài khoản test, mở `/documents`:
   - dòng bảng/card hiện storage key dưới tên, hover thấy key đầy đủ;
   - dán 1 storage key lấy từ DB vào ô tìm kiếm → còn đúng tài liệu đó;
   - dialog chi tiết vẫn hiện "File trong kho" như cũ.

## Kết quả

### Thay đổi đã làm

| File | Việc |
|---|---|
| `core/knowledge/knowledge.models.ts` | `articleSource` + `matchesArticleQuery` (hàm thuần) |
| `core/knowledge/knowledge.models.spec.ts` | 9 test: nguồn (key/URL/không có) + so khớp (tên, key, URL, rỗng) |
| `pages/documents/documents.ts` | `visible()` dùng `matchesArticleQuery`; thêm `source(article)` cho template |
| `pages/documents/documents.html` | Dòng bảng: bọc tên + `.doc-meta .doc-file` trong `.doc-name-wrap`; card lưới: `.card-meta .card-file` |
| `pages/documents/documents.scss` | `.doc-name-wrap` (grid + `min-width: 0`) và `.doc-file/.card-file` (mono); xoá rule `.empty-body` |
| `shared/i18n/vi.ts`, `en.ts` | `DOC_SEARCH_PLACEHOLDER` → "Tìm theo tên tài liệu hoặc file…" |

### 2 lỗi phát sinh khi kiểm chứng

1. **Mono bị mất do thứ tự rule**: `.doc-file/.card-file` đặt TRƯỚC nhóm
   `.doc-meta, .card-meta, .content-count` nên shorthand `font: var(--text-style-xs)`
   của nhóm reset `font-family` về Roboto (cùng specificity, rule sau thắng).
   Browser bắt: computed `font-family` ra `Roboto…` chứ không phải mono. Sửa: chuyển
   khối `.doc-file/.card-file` xuống SAU nhóm, kèm comment cảnh báo.
2. **Vượt budget style 10 kB**: bản đầu thêm 4 khai báo trùng (`font`, `color`,
   truncate) làm `documents.scss` lên 10.19 kB → 10.13 kB (warning `anyComponentStyle`).
   Theo phương án gộp thay vì nâng budget: dùng lại class `.doc-meta`/`.card-meta`
   cho dòng file (đã có font + màu + ellipsis), wrapper dùng `display: grid`, và xoá
   `.empty-body { color: var(--color-muted) }` (thừa — `.empty` đã set color, `<p>`
   con kế thừa) → build sạch, không còn warning.

### E2E thật (API :9001 + `ng serve` :4200, tài khoản `feuser`)

| Kiểm tra | Kết quả |
|---|---|
| Dòng phụ dưới tên (2 bài `API Test A/B`) | `uploads/01a1768a-2a19-743f-9dc2-a31285c3bfdb.txt`, `…7440-b970-8b03d77b37bf.txt` |
| Key dài | Cắt bằng "…", `title` giữ key đầy đủ; `scrollWidth 538 > clientWidth 373` |
| Cột bảng | Không xô lệch: x-position các cột giữ nguyên `37 / 89 / 510 / 658 / 786`, `scrollWidth == clientWidth` |
| `font-family` dòng file (bảng + lưới) | `ui-monospace, "SF Mono", Consolas, "Courier New", monospace`, 12px |
| Search `01a1768a-2a19-743f` | 2 dòng → **1 dòng** (`API Test A`) — khớp key, không chỉ khớp tên |
| Card lưới | Key nằm trong card, `scrollWidth 278 == clientWidth 278`, không tràn |
| Dialog chi tiết | Vẫn hiện "File trong kho" (mono) |

### Ghi nhận ngoài phạm vi

- `mat-button-toggle` của toolbar view (list/grid) không mang `aria-label` trên host
  dù template có binding — lỗi a11y CÓ SẴN từ trước, không sửa trong đợt này.
- Chưa verify badge trạng thái đang xử lý vì 2 bài trong DB đều `done` (badge bị ẩn).

### Verify

- `npm run lint`: All files pass linting.
- `npm test`: 19 file / 103 test pass (thêm 9 test cho `knowledge.models`).
- `npm run build`: xanh, không còn warning budget.

