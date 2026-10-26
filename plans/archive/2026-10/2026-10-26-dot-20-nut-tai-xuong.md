# Đợt 20 — Nút "Tải xuống" cho tài liệu (presigned GET)

## Vấn đề

Upload file lên rồi thì **không tải lại được file gốc**: API chỉ có presign PUT
(chiều lên), không có chiều xuống. UI chỉ có "Xem chi tiết / Chỉnh sửa / Xóa tài
liệu" — user muốn mở đúng bản PDF/DOCX mình đã nạp phải vào kho thủ công.

## Thiết kế

Dùng **presigned GET** (cùng cơ chế với chiều upload): API ký URL tạm rồi trả về,
browser tự tải từ SeaweedFS. Lý do không proxy qua API: file tối đa 500MB, stream
qua process API là tốn RAM/băng thông vô ích, và kho đã publish cổng cho máy user.

Chữ ký phải kèm `Content-Disposition: attachment` với **tên file gốc** — nếu không,
link cross-origin khiến thuộc tính `download` của browser bị bỏ qua và file mở ra
ở tab mới thay vì tải. Tên tiếng Việt phải mã theo RFC 5987 (`filename*=UTF-8''…`).

## Thay đổi

### Backend

| File | Việc |
|---|---|
| `src/common/storage/storage.go` | Thêm `PresignedGetURL(ctx, key, downloadAs string, expiry time.Duration)` vào `IStorage` |
| `src/infrastructure/seaweedfs/storage.go` | Hiện thực bằng `PresignGetObject` + `ResponseContentDisposition` (RFC 5987, escape ký tự không ASCII) |
| `src/common/constants/constants.go` | `PRESIGN_DOWNLOAD_EXPIRY = 5 * time.Minute` (tải xong là hết việc, không cần 15 phút như upload) |
| `src/controller/dtos/articles_dto.go` | `ArticleDownloadResponse{ url, expires_at, filename, content_type }` |
| `src/controller/services/articles_service.go` | `DownloadURL(ctx, id)`: chủ sở hữu qua `articleRepo.GetByID`; bài nguồn URL → 400 "tài liệu này lấy từ liên kết, không có file để tải"; HeadObject hết hạn thì mặc kệ (chữ ký ngắn); log 1 dòng `article: ký URL tải`, **không log URL** (chữ ký là credential tạm) |
| `src/controller/apis/article_controller.go` + `routers/router.go` | `GET /api/v1/articles/:id/download` + swagger annotation |
| `src/controller/services/{article_purge_service_test,chunk_stress_test}.go` | Fake storage thêm method mới |
| `docs/` | `swag init -g cmd/apis/main.go` |

### Frontend

| File | Việc |
|---|---|
| `core/knowledge/knowledge.models.ts` / `knowledge.service.ts` | `getDownloadUrl(id)` + model `ArticleDownload` |
| `pages/documents/documents.ts` | `downloadArticle(article)`: xin URL rồi bấm anchor ẩn (không `window.open` — tránh popup blocker); lỗi → toast |
| `pages/documents/documents.html` | Thêm mục "Tải xuống" vào **cả hai** menu (bảng `docMenu` + thẻ `cardMenu`), `@if` ẩn với bài nguồn URL |
| `shared/i18n/vi.ts` / `en.ts` | `DOC_ACTION_DOWNLOAD`, `DOC_DOWNLOAD_ERROR`, `DOC_DOWNLOAD_NO_FILE` |

## Không làm

- Không thêm nút tải trong dialog chi tiết (đợt sau nếu cần) — menu danh sách là
  chỗ user với tới.
- Không giới hạn số lượt ký URL / không audit download — chưa có nhu cầu.
- Không đổi `IStorage.PresignedURL` (chiều PUT) sang chữ ký chung cho hai chiều:
  hai chiều có policy khác nhau (PUT phải kèm Content-Type, GET phải kèm disposition).

## Kiểm chứng

1. `go build ./... && go vet ./... && golangci-lint run ./... && go test ./...`.
2. FE: `npm run lint`, `npm test`, `npm run build`.
3. E2E thật: upload 1 file PDF (presign → PUT → create) với **tên có dấu**
   (vd `báo cáo nội bộ.pdf`) → `GET /api/v1/articles/:id/download` → `curl -I`/`curl -o`
   cái URL đó phải trả **200**, `Content-Disposition: attachment` mang đúng tên
   (kiểm cả `filename*=UTF-8''…`), bytes tải về khớp `Content-Length` với file gốc.
4. E2E phủ định: bài tạo từ `url` → endpoint trả 400 thông điệp rõ; bài của user
   khác → 404 (không lộ sự tồn tại).

## Kết quả

### Đã làm

- `IStorage` thêm `PresignedGetURL(ctx, key, downloadAs, expiry)`; SeaweedFS ký bằng
  `PresignGetObject` + `ResponseContentDisposition` (không sửa metadata object — header
  GET-override nằm ngay trong chữ ký).
- Bộ mã header tách sang `src/infrastructure/seaweedfs/disposition.go` vì đó là phần dễ
  sai nhất: `attachment` (link cross-origin làm browser bỏ thuộc tính `download`), bản
  ASCII `filename="…"` cho client cổ, bản `filename*=UTF-8''…` theo RFC 5987 cho tên có
  dấu, fallback `"download"` khi tên thuần dấu (không có ASCII nào còn lại).
- `PRESIGN_DOWNLOAD_EXPIRY = 5 phút` (ngắn hơn upload: chữ ký chỉ cần sống đủ cho một cú
  click), `ArticleDownloadResponse{url, expires_at, filename, content_type}`,
  `articleService.DownloadURL`, `GET /api/v1/articles/:id/download` + swagger đã regen.
- `downloadFilename`: tên bài đã kèm đuôi thì dùng nguyên văn; bài cũ bị FE cắt đuôi thì
  nối lại đuôi từ storage key — tải về vẫn mở đúng app.
- FE: `getDownloadUrl` + `ArticleDownload`/`toArticleDownload`, `downloadArticle()` bấm
  anchor ẩn (không `window.open`), mục "Tải xuống" trong **cả hai** menu, `@if hasFile`
  ẩn với bài nguồn URL. Test FE mới: `articleHasFile` (3), `getDownloadUrl` map + thiếu
  field (2) → 108/108 xanh.
- **Lệch so với kế hoạch**: không thêm key `DOC_DOWNLOAD_NO_FILE` — nút đã bị ẩn với bài
  nguồn URL nên đường 400 là bất khả dĩ trong UI, giữ một key không ai dùng là nợ vô ích.

### Kiểm chứng

- `go build` / `go vet` / `golangci-lint run ./...` → `0 issues.`; `go test ./...` → 20
  package ok, 0 FAIL. Test mới: 6 case `DownloadURL` (ký đúng key/tên/hạn, nối lại đuôi,
  URL-source 400 + không ký, bài người khác 404 + không ký, lỗi kho trả thẳng lên trên) và 3 case
  `buildContentDisposition`.
- `npm run lint` → All files pass linting; `npm test` → 19 files/108 tests; `npm run build`
  OK. `npx prettier --write` các file đã sửa → không đổi gì thêm.
- **Ngân sách hàm**: `NewRouter` 87 → 89 dòng vì thêm một dòng route — bảng route thuần
  (1 điểm quyết định) phình theo số API, nên allowlist được nâng có comments giải thích
  thay vì cắt hàm thành từng mảnh vô nghĩa.

### E2E thật (API 9001 + SeaweedFS + Postgres dev)

1. Login `dot19user` (id 87) → tạo danh mục `dot20-cat` (id 126) → presign batch +
   PUT `quy_dinh_noi_bo.pdf` (200) → `POST /articles` với tên **"báo cáo nội bộ đợt 20.pdf"**
   (25 ký tự / 34 byte UTF-8) → bài **id 69**, response trả về `name -eq` đúng chuỗi gửi lên.
2. `GET /articles/69/download` → `filename` 25 ký tự/34 byte, `expires_at` = now + 5 phút,
   `content_type: application/pdf`.
3. `curl -o` chính cái URL đã ký → **HTTP 1.1 200 OK**,
   `Content-Disposition: attachment; filename="bo co ni b t 20.pdf"; filename*=UTF-8''b%C3%A1o%20c%C3%A1o%20n%E1%BB%99i%20b%E1%BB%99%20%C4%91%E1%BB%A3t%2020.pdf`,
   `Content-Length: 52659`, `Content-Type: application/pdf`; file tải về **SHA256 khớp
   bản gốc** (`sha_match=True len=52659`).
4. Phủ định: bài tạo từ `url` (id 70) → **400** `"tài liệu này lấy từ liên kết, không có
   file để tải"`; user khác (id 88 `dot20other`) xin tải bài 69 → **404** "bài viết không
   tồn tại" (không lộ sự tồn tại); sửa 1 ký tự của `X-Amz-Signature` → **403** từ
   SeaweedFS (chứng tỏ đây là chữ ký thật, không phải URL public).
5. Log: `article: ký URL tải file {trace_id, article_id, name, file, user_id, expires_ms}`
   — tên tiếng Việt trong log đúng dấu; **trong 496 dòng log không có một dòng nào chứa
   `X-Amz-Signature` hay field `url`** → chữ ký không rò ra log.

### Dọn dẹp

- Đã xóa bài 69 + 70 và danh mục 126 qua API. Worker không chạy trong lượt test nên blob
  `uploads/01a1774e-…pdf` còn nằm lại bucket dev `app-uploads`; janitor purge sẽ dọn khi
  worker chạy lại (quá `PURGE_GRACE_PERIOD`).
- User `dot20other` (id 88) còn lại trong DB dev: repo chưa có route xóa user
  (`DeleteUser` vẫn `panic("implement me")`) nên không xóa được bằng API — ghi nhận lại,
  không tự code mới trong đợt này.
- Đã dừng API (PID 22496) và xóa sạch file tạm `dot20*` trong TEMP.
