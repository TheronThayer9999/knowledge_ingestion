# Đợt 10 — Upload nhiều file một lượt + log theo file/stage

## Bối cảnh

Người dùng báo 3 việc:

1. Hộp thoại upload chỉ chọn được **1 file** một lần, muốn chọn nhiều.
2. Log pipeline **không nói đang xử lý file nào, giai đoạn nào** — chỉ có
   `extract: xong 1 kỳ quét {found: 0, extracted: 0}` rồi `worker: job xong`.
3. `WARN SLOW SQL` in kèm **stack trace đầy đủ** nên trông như panic runtime.

Hai câu hỏi còn lại đã trả lời bằng số liệu, không cần sửa code:

- Embedding **vẫn chạy** bge-m3 (`ollama: gọi embed` + `articles.index_done_chunks`
  7/10/11/947/7036/8355); chỉ bảng `embed_cache` bị bỏ (hit rate ~100%, count=1).
- LLM sau khi "tiền xử lý xong" là **phase 2 `compile`** (mỗi mục 1 lần tóm tắt,
  cache Redis 30 ngày), chạy sau index chứ không phải index gọi lại bge-m3.

## Hiện trạng upload

| Thành phần | Trạng thái |
|---|---|
| `POST /uploads/presign` (1 file, field `file`) | có |
| `POST /uploads/presign/batch` (**≤ 20 file**, field `files`) | **có sẵn**, không ai gọi |
| `POST /articles` | chỉ 1 bài/lần |
| FE dialog | 1 file: input không `multiple`, `onPick` lấy `files?.[0]` |
| Test cho `fileService` | không có |

⇒ Phần **backend đủ dùng**, đợt này chỉ làm FE + log + sửa stack trace.

## Phần A — FE chọn & upload nhiều file

### `core/knowledge/knowledge.service.ts`

- Thêm `uploadDocuments(batch, onProgress)`:
  `presignMany(files)` (field `files`) → `concatMap` từng vé → `putObject` →
  `createArticle`; **`catchError` trong từng file** để 1 file hỏng không giết cả lô,
  trả `UploadFileOutcome[]` (`{file, article | null, error}`) qua `toArray()`.
- `UploadProgress = { index: number | null; stage: UploadStage }` — `index === null`
  là bước của cả lô (presign chạy 1 request); có index là bước của 1 file.
- Xoá `uploadDocument`/`presign` (single) vì sau khi đổi dialog không còn caller —
  để lại là dead code.

### `pages/documents/dialogs/upload-dialog.*`

- Bỏ `name` khỏi form chung, thay bằng **danh sách dòng**, mỗi dòng:
  `{ file, name, status, error }`, `status ∈ pending|uploading|creating|done|error`.
- Tên mặc định = tên file bỏ đuôi, **sửa được từng dòng** (input thường + `(input)`,
  không dùng FormArray cho gọn).
- Chọn/ kéo thả nhiều file; validate client (đuôi + 500MB) như cũ, cộng trần
  **20 file/lô** (khớp `MAX_UPLOADS_PER_REQUEST`) → báo `DOC_UPLOAD_MAX_FILES`.
- Trong lô: 1 file lỗi thì **chạy tiếp** các file còn lại; dòng lỗi hiện lý do,
  dialog **không đóng** để bấm Upload lại (chỉ upload dòng chưa `done`).
- Lô xong hết → `dialogRef.close(articles)`; `documents.ts` nhận `Article[]`,
  toast `DOC_UPLOAD_SUCCESS`, chọn thư mục theo `articles[0].categoryId`, `load()` 1 lần.
- Giữ dải 3 bước (presign → upload → create) làm tiến trình chung; presign điều khiển
  bằng event `index === null`, hai bước sau theo từng dòng.

### i18n (vi + en, cùng thứ tự)

`DOC_UPLOAD_ADD_MORE`, `DOC_UPLOAD_MAX_FILES`, `DOC_UPLOAD_REMOVE_FILE`,
`DOC_UPLOAD_ROW_PENDING|UPLOADING|CREATING|DONE|ERROR`, `DOC_UPLOAD_PARTIAL`,
`DOC_UPLOAD_RETRY`.

### Test FE

`core/knowledge/knowledge.service.spec.ts` (vitest + `HttpTestingController`):

1. 2 file → 1 POST `…/uploads/presign/batch` với 2 part `files` → 2 vé → 2 PUT đúng
   `Content-Type` → 2 POST `/articles` **tuần tự** → kết quả 2 article.
2. PUT của file thứ 2 lỗi 500 → `outcome[0].article` có, `outcome[1].error` có
   (lô không dừng).

## Phần B — Log theo file và theo stage

Nguyên tắc: mọi log **per bài** phải mang `article_id` + `name`; mỗi bài có 1 dòng
**bắt đầu** để biết file nào đang chạy; đầu mỗi kỳ quét log **danh sách bài đã nhận**.

- `article_extract_service.go`: `extract: nhận lô bài {count, articles: ["id:name"…]}`,
  `extract: bắt đầu bài {article_id, name}`, thêm `name` cho `tiến trình`, `xong 1 bài`,
  `tiếp tục từ mảnh đã trích`, các `Error/Warnw` per bài.
- `article_index_service.go`: tương tự (`index: nhận lô bài`, `index: bắt đầu bài`,
  `index: tiến trình`, `index: xong 1 bài`, `ollama: gọi embed`/`embed xong`).
- `article_compile_service.go`: `compile: nhận lô bài`, `compile: bắt đầu bài`,
  thêm `name` cho `xong 1 bài` / `bài không có mục nào` / lỗi cache.
- Helper `articleTags([]*domain.Article) []string` đặt trong
  `article_extract_service.go` (dùng chung 3 phase, cùng package).

Không đổi nội dung/ level log hiện có, chỉ thêm field.

## Phần C — Stack trace chỉ từ ERROR

`logs.LoadLogger` dùng `zap.NewDevelopmentConfig()`; `Config.buildOptions` đặt
`stackLevel = WarnLevel` khi `Development` → mọi WARN (gồm SLOW SQL) có stack.

- Tách `newLogger(cfg zap.Config) (*zap.Logger, error)` = `cfg.Build(zap.AddStacktrace(zapcore.ErrorLevel))`
  (`Config.Build` áp `opts` **sau** `buildOptions` nên option này thắng).
- `LoadLogger` gọi `newLogger(cfg)` với `cfg.DisableStacktrace = true` cho chắc
  (không phụ thuộc thứ tự) — kết quả: WARN sạch, ERROR vẫn có stack.
- `logs_test.go`: build config ghi ra file tạm qua `newLogger`, log 1 WARN + 1 ERROR,
  assert 1 dòng `stacktrace` duy nhất và nó thuộc dòng ERROR.

## Kiểm chứng

- Go: `go build ./...`, `go vet ./...`, `golangci-lint run ./...`, `go test ./...`.
- FE: `npm run lint`, `npm test`, `npm run build` (trong `knowledge_ingestion_fe`).
- Tay: chọn 3 file (1 file sai đuôi) → 2 file lên danh sách kèm lỗi; upload → 2 dòng
  xanh, danh sách tài liệu refresh 1 lần; log worker hiện `extract: bắt đầu bài {name}`;
  WARN SLOW SQL không còn stack.

## Commit

1. `docs(plans): lên kế hoạch đợt 10`
2. `feat(upload): chọn và upload nhiều file trong một lượt` (FE + i18n + spec)
3. `feat(worker): log tên file và từng bài theo stage`
4. `fix(logs): chỉ gắn stack trace từ mức error`
5. `docs(plans): ghi kết quả đợt 10`

## Ngoài phạm vi (ghi nhận, chưa làm)

- Không thêm `POST /articles/batch` (1 transaction cho N bài) — FE gọi N lần như hiện tại.
- Không có janitor dọn object S3 mồ côi (PUT xong mà tạo article fail) — rác ở
  `uploads/` chưa được quét; `purge` chỉ xoá theo bài đã tồn tại.
- Không bật `worker.enhance_keywords` (đường LLM **mỗi chunk** đang tắt).
