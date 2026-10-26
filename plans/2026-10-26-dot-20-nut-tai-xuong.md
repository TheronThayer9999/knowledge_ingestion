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

(chờ chạy xong)
