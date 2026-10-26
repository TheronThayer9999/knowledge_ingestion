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

## Kết quả

### Đã đổi

| File | Thay đổi |
|---|---|
| `article_extract_service.go` | thêm `fileTag` (StorageKey → URL → `"-"`), `articleTag` (`id:tên (file)`); 11 dòng log theo bài có `file`, dòng bắt đầu/xong bài có thêm `doc_type`/`content_type`; nhánh backoff đổi `storage_key` → `file` |
| `article_index_service.go` | 12 dòng: bắt đầu bài (+`doc_type`/`content_type`), hết giờ, lỗi vĩnh viễn, backoff, không đánh dấu lỗi/done, panic, tiến trình, xong 1 bài, embed title lỗi, gọi embed, embed xong — đều có `file` |
| `article_compile_service.go` | 13 dòng (gồm `compile: gọi LLM tóm tắt mục`, cache hit, đọc/ghi cache lỗi, panic, không có mục nào, xong 1 bài) — đều có `file` |
| `file_service.go` | `signOne` (dùng chung ký đơn + ký lô) log `file: ký URL upload` với `filename`/`key`/`size`/`content_type`, lỗi thì WARN cùng bộ field — KHÔNG log `upload_url` |
| `articles_service.go` | tách `detectDocType` + `logArticleCreated`; `Create` log `article: tạo bài, chờ worker` với `article_id`/`name`/`file`/`content_type`/`doc_type`/`category_id`/`user_id` |
| `article_log_tag_test.go` | test mới `TestFileTag` (4 ca) + `TestArticleTags` (tag lô có file) |

`Create` nhờ tách helper đã xuống dưới ngân sách hàm (60 dòng / 12 điểm) nên **xoá entry
`(*articleService).Create` khỏi `functionBudgetAllowlist`** — danh sách nợ chỉ ngắn đi.

### Test thật (end-to-end)

API :9001 (`configs/config.local.json`) + worker chạy song song; upload qua `POST /uploads/presign`
→ `PUT` presigned → `POST /articles` (user feuser/44, danh mục 124).

| # | Bài | Việc | Kết quả |
|---|---|---|---|
| 1 | 65 `dot15-log-file` | `txt_noi_dung.txt` (6.5 MB) | extract xong 25 trang; index chạy tới 6880 chunk thì **dừng chủ động** (file quá lớn cho một lượt test) — xoá bài, purge dọn luôn (`found 1, purged 1`) |
| 2 | 66 `dot15-log-file-2` | `quy_che_tree.txt` (2.6 KB) | **đủ 3 phase**: extract 1 trang → index 12 chunk → compile 11 mục; `compile: gọi LLM tóm tắt mục` hiện `file` cho từng mục |
| 3 | 67 `dot15-log-file-3` | cùng file, chạy lại trên binary build lại | extract/index/compile đều có `file` (compile ăn cache: `compile: cache hit, không gọi LLM` cũng có `file`) |

Log API (đọc được cả tiếng Việt sau khi đặt `[Console]::OutputEncoding` = UTF-8):

```
12:35:14.370 INFO services/file_service.go:119 file: ký URL upload
  {"trace_id":"01a176b4-abc1-…","filename":"quy_che_tree.txt",
   "key":"uploads/01a176b4-abc2-784c-8a0b-e26c095559fe.txt","size":2633,"content_type":"text/plain"}
12:35:19.246 INFO services/articles_service.go:123 article: tạo bài, chờ worker
  {"trace_id":"01a176b4-bebf-…","article_id":66,"name":"dot15-log-file-2",
   "file":"uploads/01a176b4-abc2-784c-8a0b-e26c095559fe.txt","content_type":"text/plain",
   "doc_type":"general","category_id":124,"user_id":44}
```

Log worker — cùng một `file` xuyên 3 phase (bài 66):

```
12:35:21.959 INFO extract: nhận lô bài      {"articles": ["66:dot15-log-file-2 (uploads/01a176b4-abc2-….txt)"]}
12:35:21.959 INFO extract: bắt đầu bài     {"article_id":66,"name":"dot15-log-file-2","file":"uploads/01a176b4-abc2-….txt","doc_type":"general","content_type":"text/plain"}
12:35:21.986 INFO extract: xong 1 bài      {…,"file":"uploads/01a176b4-abc2-….txt","doc_type":"general","pages":1,"new_pages":1,"elapsed_ms":26}
12:35:31.969 INFO index: bắt đầu bài       {"article_id":66,"name":"dot15-log-file-2","file":"uploads/01a176b4-abc2-….txt","doc_type":"general","content_type":"text/plain"}
12:35:32.700 INFO index: xong 1 bài        {…,"file":"uploads/01a176b4-abc2-….txt","pieces":1,"chunks":12,"elapsed_ms":730}
12:36:… INFO compile: bắt đầu bài     {"article_id":66,"name":"dot15-log-file-2","file":"uploads/01a176b4-abc2-….txt","doc_type":"general","content_type":"text/plain"}
12:35:36.929 INFO compile: gọi LLM tóm tắt mục
  {"article_id":66,"name":"dot15-log-file-2","file":"uploads/01a176b4-abc2-….txt",
   "section":"CHƯƠNG I: PHẠM VI VÀ ĐỐI TƯỢNG","chars":85,"model":"kc/nvidia/nemotron-3.5-super-vl:free"}
12:36:52.300 INFO compile: xong 1 bài    {…,"file":"uploads/01a176b4-abc2-….txt","sections":11}
```

DB xác nhận bài 66 kết thúc `extract/index/compile = done`, `page_count=1`, 11 dòng `article_sections`.

### Verify

- `go build ./...`, `go vet ./...` sạch; `golangci-lint run ./...` **0 issues**; `go test ./...` xanh toàn bộ
  (gồm `src/quality` sau khi xoá allowlist entry ở trên).
- `codegraph sync` sau khi sửa Go.

### Dọn dẹp

- Bài test 65/66/67 đã `DELETE /api/v1/articles/:id` (soft delete); 65 đã purge hẳn, 66/67 chờ purge
  theo retention. File fixture dùng lại trong `loadtest/fixtures/` (không có file rác mới trong repo).
- Log tạm của lượt test nằm ở `%TEMP%`, không vào repo.
