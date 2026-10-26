# Đợt 19 — Giữ nguyên tên file khi upload

## Vấn đề

User chọn file `Báo cáo quý 3.pdf` nhưng tài liệu tạo ra lại tên `Báo cáo quý 3` — mất
phần mở rộng. Nguyên nhân nằm ở FE: `upload-dialog.ts` prefill tên dòng bằng
`stripExtension(file.name)` (hàm tự viết chỉ để cắt đuôi), trong khi BE lưu `name`
nguyên văn (`dto.ToModel()` → `articles.name`), không cắt gì.

Hệ quả phụ (đáng sửa luôn):

- `chunker.DetectType(contentType, name)` dò `.xlsx`/`.csv` **trong tên tài liệu** để
  chọn template chunking kiểu `table`. Tên bị cắt đuôi ⇒ mất tín hiệu này (chỉ còn
  dựa vào content-type).
- Người dùng nào muốn upload cả `quy_che.pdf` và `quy_che.txt` thì 2 tài liệu trùng
  tên y hệt nhau trên danh sách — không phân biệt được.

## Thay đổi

| File | Việc |
|---|---|
| `knowledge_ingestion_fe/.../documents/dialogs/upload-dialog.ts` | Prefill tên dòng = `file.name` (nguyên văn, có cả đuôi); xoá `stripExtension` (không còn chỗ dùng) |
| `src/common/chunker/template_test.go` (mới) | Khoá lí do phải giữ đuôi: `DetectType` chỉ ra `table` khi tên còn `.xlsx`/`.csv` (hoặc content-type là csv/excel) |

Không đổi BE: `articles.name` vốn đã lưu nguyên văn, `binding:"required,min=1,max=255"`
vẫn hợp lệ với tên có đuôi (input FE đã có `maxlength="255"`).

## Không làm

- **Không** nhét tên file vào `storage_key` hiện tại (`uploads/<uuidv7><ext>`): key do
  BE sinh để chống ghi đè/path traversal, `utils.SanitizeFileName` chỉ giữ ASCII nên
  tên tiếng Việt sẽ thành `tri_th_c_tay_ph__ng` — xấu hơn là có ích. Tên gốc đã được
  giữ ở `articles.name` và hiện ở danh sách tài liệu.
- Không đổi format hiển thị danh sách (việc kiểm UI/UX user tự làm).
- Không tự thêm/sửa đuôi ở BE — tên do user sửa trong dialog là tên cuối cùng.

## Kiểm chứng

1. `cd knowledge_ingestion_fe; npm run lint; npm test; npm run build`.
2. Grep lại: không còn `stripExtension` trong `src/`.
3. E2E thật (không browser): `POST /api/v1/uploads/presign/batch` với file
   `triethoc_dong_phuong_va_so_sanh.pdf` → PUT lên kho → `POST /api/v1/articles` với
   `name` = đúng tên file (đúng như FE giờ gửi) → `GET /api/v1/articles` trả về
   `name` còn nguyên đuôi `.pdf`, và `doc_type`/`content_type` vẫn như cũ.

## Kết quả

### Kiểm tra

- `npm run lint` → *All files pass linting*; `npm test` → 19 file / 103 test pass;
  `npm run build` OK.
- `go test ./src/common/chunker -run TestDetectTypeTheoTenFile -v` → PASS (5 ca).
- Grep `stripExtension` trong `knowledge_ingestion_fe/src` → không còn.

### E2E thật qua API (không dùng browser)

User test `dot19user` (id 87), category `dot19-cat` (id 125), file thật
`docs_test_case_and_data/Tài Liệu Mẫu/triethoc_dong_phuong_va_so_sanh.pdf`.

1. `POST /api/v1/uploads/presign/batch` → `filename: "triethoc_dong_phuong_va_so_sanh.pdf"`,
   `key: uploads/01a17724-65f4-...pdf`, `content_type: application/pdf`.
2. `PUT <upload_url>` → **200** (file nằm trong kho SeaweedFS).
3. `POST /api/v1/articles` với `name` = tên file đầy đủ (đúng như FE giờ gửi) → bài `id=68`,
   response trả `"name":"triethoc_dong_phuong_va_so_sanh.pdf"`.
4. `GET /api/v1/articles?category_id=125` → `name` vẫn còn `.pdf`.
5. DB: `select name, content_type from articles where id=68` →
   `triethoc_dong_phuong_va_so_sanh.pdf | application/pdf`.

Kết luận: tên file user đặt được giữ nguyên từ dialog → presign → article → danh sách,
không còn bị cắt đuôi; `content_type` vẫn do BE suy từ đuôi như cũ.

### Dọn dẹp

Đã xoá mềm bài `68` + category `125` của user test (API `DELETE`), dừng API dev, xoá file
log/json tạm — không để lại dữ liệu rác trong DB dev.
