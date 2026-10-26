# Đợt 6 — Trích `.xlsx`/`.pptx` + chặn `.zip` (đồng bộ whitelist upload ↔ extractor)

## Vì sao

`file_service.allowedExts` cho upload **13 đuôi**, `extractor.ExtractRawPages` chỉ trích được **10**.
Ba đuôi lọt lưới: `.xlsx`, `.pptx`, `.zip` → upload thành công, worker extract gặp `ErrUnsupported`,
bài nằm ở `failed` và người dùng chỉ thấy badge "Thất bại", không biết vì sao. Đúng nhóm lỗi im lặng
mà 3 đợt gần đây đang dọn (RAG_RESEARCH §6.1 #4).

Hướng xử lý chọn theo giá trị thực:

| Đuôi | Quyết định | Lý do |
|---|---|---|
| `.xlsx` | **Hỗ trợ** | Biểu phí/mức phạt/định mức nằm trong bảng Excel là use case thật; stdlib `archive/zip` + `encoding/xml` đủ, không thêm dep |
| `.pptx` | **Hỗ trợ** | Slide là văn bản in bình thường; cùng kỹ thuật zip+XML |
| `.zip` | **Chặn ở upload** | Không phải tài liệu; trích archive = mở đường zip-bomb/duyệt file lạ, không có giá trị nghiệp vụ |

## Đầu ra phải giữ đúng quy ước đang có

- `Num = 0` (không phân trang), chia mảnh bằng `visitPieces` ⇒ `Seq` tăng dần, RAM hằng số.
- Bảng ra markdown pipe `| a | b |` giống `extractDocx` ⇒ chunker cắt theo hàng, LLM thấy cấu trúc.
- **Ô trống giữ chỗ theo cột** (`A1`, `C1` ⇒ `| a |  | c |`): Excel hay để cột giữa trống, bỏ trống là
  dữ liệu xô lệch, "mức phạt" rơi vào chỗ "hành vi".
- Sheet/slide có tiêu đề ngăn cách (`## Sheet: Biểu phí`, `## Slide 3`) để chunk mang ngữ cảnh — tên sheet
  đọc từ `xl/workbook.xml` + rels, thiếu thì fallback `## Sheet <n>`.

## Thay đổi

| Nơi | Việc |
|---|---|
| `src/common/extractor/office.go` | `openOfficeZip` (đọc archive có trần, lỗi `ErrInvalid`), `zipReader` cho 1 entry, `sortEntriesByNumber` |
| `src/common/extractor/xlsx.go` | `extractXlsx`: sharedStrings + workbook/rels (tên sheet) + từng sheet → pipe rows |
| `src/common/extractor/pptx.go` | `extractPptx`: `ppt/slides/slideN.xml` sắp theo số → paragraph từ `<a:t>` |
| `src/common/extractor/extractor.go` | thêm case `.xlsx`/`.pptx`; `Supports(key)` + `SupportedExts()` |
| `src/controller/services/file_service.go` | bỏ `.zip` khỏi `allowedExts` (+ `sniffAllow` nếu có) |
| `src/controller/services/file_service_sync_test.go` | test chặn lệch: **mọi** đuôi trong `allowedExts` phải `extractor.Supports` |
| `knowledge_ingestion_fe/.../upload-dialog.ts` | bỏ `zip` khỏi `ACCEPTED_EXTS` (FE đang cho chọn `.zip`) |
| `RAG_RESEARCH.md` §6.1 #4 | đánh dấu đã xử lý + cách xử lý |

## Không làm (giữ phạm vi)

- Không đọc công thức/định dạng số (giữ giá trị thô `<v>`; ô ngày ra serial — chấp nhận, ghi rõ trong comment).
- Không OCR ảnh nhúng trong Excel/PPT — ca đó đi qua tool agent (đợt 5) nếu trang bị thiếu.
- Không hứa "bảng Excel thông minh" (gộp ô, nhiều header) — đó là #24/L2, làm sau khi có eval bảng.

## Nghiệm thu

- [x] `go test ./src/common/extractor/` có test: 2 sheet + shared strings + ô trống giữ cột + tên sheet;
      pptx 3 slide sắp đúng thứ tự số (2 trước 10); zip thiếu entry ⇒ `ErrInvalid`; `ExtractAll` với
      key `.xlsx`/`.pptx` trả text như mong đợi.
- [x] Test đồng bộ whitelist ↔ extractor xanh (thêm `.zip` lại là test đỏ).
- [x] `go build ./...`, `go vet ./...`, `golangci-lint run ./...`, `go test ./...` xanh; FE `npm run lint` xanh.

## Kết quả (25/10/2026)

- `office.go`: thêm `readZipEntry` (đọc 1 entry có trần) và `extractOffice` (dispatch 3 loại OOXML).
- `xlsx.go`: sharedStrings (kể cả chuỗi giàu định dạng), tên sheet qua `workbook.xml` + rels
  (Target tương đối **và** tuyệt đối), fallback `## Sheet <n>`; giữ vị trí cột theo `r="C5"`;
  ô công thức chỉ lấy `<v>`; ô xuống dòng được ép về 1 dòng.
- `pptx.go`: `ppt/slides/slideN.xml` sắp theo SỐ, mỗi `<a:p>` một đoạn, `## Slide <n>`.
- `extractor.go`: `supportedExts` + `Supports`/`SupportedExts` là cổng chặn duy nhất;
  switch chỉ còn nhánh thực thi, nhánh `default` trả `errNoHandler` để test phát hiện lệch.
- `file_service.go`: bỏ `.zip`; `file_service_sync_test.go` giữ whitelist ⊆ extractor.
- FE `upload-dialog.ts`: bỏ `'zip'` khỏi `ACCEPTED_EXTS`.
- Test mới: `office_test.go` (5 test + `TestSupportedExtsDispatched`), `file_service_sync_test.go` (2 test).
  Toàn bộ suite xanh, lint 0 issue.

## Commit

1. `docs(rag): add batch 6 plan for office extraction`
2. `feat(extractor): read xlsx and pptx with stdlib zip and xml`
3. `fix(upload): drop zip from whitelist and guard extractor parity`
