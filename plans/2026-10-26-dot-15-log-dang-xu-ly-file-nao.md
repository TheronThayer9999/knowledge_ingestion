# Đợt 15 — Log rõ đang xử lý FILE nào (API + worker)

## Bối cảnh

Log hiện tại đã có `article_id` + `name` ở gần hết dòng của worker, nhưng:

- `name` là **tên bài người dùng đặt** (FE cho sửa trong dialog upload), không phải file. Bài do
  script/API tạo có `name = "API Test A"` trong khi blob thật là `uploads/01a1768a-….txt`.
- `storage_key` chỉ xuất hiện ở **một** dòng (nhánh lỗi `extract: bỏ qua bài`). Muốn biết "đang xử
  lý file nào" (blob nào trên SeaweedFS) vẫn phải query DB.
- Phía API: `POST /api/v1/uploads/presign[/batch]` và `POST /api/v1/articles` **không log dòng nào**
  cho từng file — upload 3 file thì log chỉ có 1 dòng HTTP request, không biết file nào lên kho,
  file nào thành bài nào.
- Dòng theo lô (`extract/index/compile: nhận lô bài`) in `id:tên` nên cũng không phân biệt được file.

Yêu cầu đã ghi trong memory "File Processing Logging Requirement": log phải nói rõ file nào đang
được xử lý.

## Mục tiêu

1. Mỗi dòng log gắn với 1 bài phải có thêm field **`file`** = `articles.storage_key` (blob thật,
   dạng `uploads/<uuid>.<ext>`) hoặc URL khi bài nguồn link ngoài.
2. Dòng **bắt đầu mỗi phase** thêm `doc_type` + `content_type` (biết ngay file gì, chunker nào).
3. Dòng theo lô đổi tag thành `id:tên (file)`.
4. Phía API log 1 dòng/file ở bước ký URL và 1 dòng khi tạo bài (id + file + doc_type + category).
5. **Không log presigned URL** (chứa chữ ký) và không log nội dung file.

## Thay đổi

### `src/controller/services/article_extract_service.go` (nơi đặt helper log của worker)

- `fileTag(a *domain.Article) string`: `StorageKey` nếu có, ngược lại `URL` (rỗng cả hai → `"-"`).
- `articleTag(a) string` = `"<id>:<name> (<file>)"`; `articleTags` dùng lại hàm này.
- Thay `"name", article.Name` bằng cặp `"name", article.Name, "file", fileTag(article)` ở mọi dòng
  của extract (bắt đầu bài, tiến trình, xong bài, hết giờ, backoff, panic, không lưu được metadata,
  bài bị xoá giữa chừng, lỗi embed title…). Dòng bắt đầu thêm `doc_type`, `content_type`.
- Dòng `extract: xong 1 bài` thêm `pages`; dòng `extract: tiến trình` thêm `file` (đang ở trang nào
  của file nào).

### `article_index_service.go` / `article_compile_service.go`

- Cùng luật: mọi dòng theo bài thêm `file`, dòng bắt đầu thêm `doc_type`/`content_type`.
- `compile: gọi LLM tóm tắt mục` thêm `file` — dòng này lặp hàng chục lần trong một bài, thiếu file
  là không biết LLM đang tóm tắt file nào.

### `file_service.go`

- `PresignUpload` / `PresignUploads`: mỗi file 1 dòng `file: ký URL upload` với
  `filename`, `key`, `size`, `content_type` (không log `upload_url`).

### `articles_service.go`

- `Create`: sau khi ghi row thành công, log `article: tạo bài, chờ worker` với `article_id`, `name`,
  `file`, `content_type`, `doc_type`, `category_id`, `user_id` — nối API ↔ worker bằng cùng `file`.

## Test

Unit (pure function, theo convention "Pure Function Testing Strategy"):

- `fileTag`: storage key `uploads/x.txt` → nguyên key; bài URL → URL; cả hai rỗng → `"-"`.
- `articleTags`: nhiều bài → `["63:API Test A (uploads/….txt)"]`.

Thật (end-to-end, đúng yêu cầu "log ra đang xử lý file nào"):

1. API 9001 (config.local) + worker chạy song song.
2. Upload 1 file nhỏ qua `POST /uploads/presign` → `PUT` presigned → `POST /articles`
   (name `dot15-log-file`): kiểm log API có 2 dòng kèm `filename`/`key`.
3. Worker xử lý bài đó: kiểm 3 dòng bắt đầu phase (`extract:`, `index:`, `compile:`) đều có
   `file = uploads/<uuid>.txt`, `doc_type`, `content_type`; dòng `compile: gọi LLM tóm tắt mục` có
   `file`.
4. Dọn: xoá bài test qua `DELETE /api/v1/articles/:id` (soft delete — purge theo retention).

Verify: `go build ./... && go vet ./... && golangci-lint run ./...` + `go test ./...`; `codegraph sync`.

## Rủi ro / ghi chú

- Chỉ thêm field + dòng log, không đổi hành vi/kiểu dữ liệu nào; không đụng message cũ (đợt 10/11
  đã dùng tên message làm mốc grep) — chỉ thêm `file` và làm giàu dòng lô.
- `file` là storage key, KHÔNG phải presigned URL (URL có chữ ký, hết hạn, lộ credential).
- Log phía API không có `trace_id` sẵn — dùng `utils.TraceIDFromCtx(ctx)` như `Render` để nối với
  dòng HTTP request cùng request.
