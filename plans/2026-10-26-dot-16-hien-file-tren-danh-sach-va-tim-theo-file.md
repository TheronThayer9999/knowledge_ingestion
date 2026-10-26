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
