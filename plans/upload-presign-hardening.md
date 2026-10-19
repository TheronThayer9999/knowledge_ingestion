# Kế hoạch: gia cố luồng upload (presign → S3 → tạo bài)

Trạng thái: **chờ thực hiện** (chưa có thay đổi code cho kế hoạch này).
Ngày lập: 2026-10-19.

## 1. Luồng hiện tại

1. Client `POST /uploads/presign` (multipart, gửi **toàn bộ file**).
2. API đọc hết stream để lấy size + 512 byte đầu, sniff MIME, trả URL PUT đã ký (`file_service.go`).
3. Client `PUT` thẳng lên S3 (SeaweedFS) bằng URL đó.
4. Client `POST /articles` với `storage_key`. Server `HeadObject` kiểm tra tồn tại, chặn trùng theo user (`articles_service.go`).
5. Worker phase 0 `Download` từ S3 rồi extract (`article_extract_service.go`), phase 1 index.

Hướng chung (đẩy thẳng lên S3, worker đọc lại từ S3) là đúng. Phần cần sửa là bước 1–4.

## 2. Vấn đề cần sửa

| # | Vấn đề | Bằng chứng trong code | Rủi ro |
|---|---|---|---|
| P1 | Presign bắt client gửi toàn bộ file qua API | `PresignUpload` dùng `MultipartReader` + `ReadHeadAndSize` | File đi qua API 2 lần, giữ kết nối lâu, tốn băng thông |
| P2 | Presigned PUT không ký `Content-Length` | `storage.go` `PresignedURL` chỉ set `ContentType` | Sau khi presign, client PUT file to hơn size đã khai, hoặc nội dung khác loại |
| P3 | `create_article` không kiểm tra size/nội dung của object | Chỉ có `Exists` (HeadObject) | Bài có thể trỏ tới blob sai loại hoặc sai size |
| P4 | Không ràng buộc key với user đã presign | `ExistsByStorageKey` chỉ chặn trùng trong cùng user | User A đính key của user B vào bài của mình |
| P5 | Blob mồ côi khi presign xong mà không tạo bài | Không có lifecycle/job dọn | Tốn dung lượng S3 |

Ngoài phạm vi kế hoạch này (ghi nhận để theo dõi):
- **RAM worker khi extract PDF**: `gopdf` chỉ có `OpenBytes`, PDF 90MB đỉnh ~920MB. Hiện chặn bằng `maxPDFBytes` 100MB và `EXTRACT_POOL_SIZE = 1`. Chưa đạt yêu cầu "RAM không phụ thuộc kích thước file". Cần thư viện PDF đọc theo khoảng byte, làm riêng.
- **Nhiều replica worker**: claim có lease, cần test riêng khi chạy 2+ instance.

## 3. Thiết kế đề xuất

### 3.1 Presign bằng JSON (sửa P1)
- `POST /uploads/presign` nhận JSON:
  ```json
  { "filename": "a.pdf", "size": 94112345, "head_base64": "JVBERi0x..." }
  ```
  `head_base64` là tối đa 512 byte đầu, client tự cắt.
- Server: validate `size` (> 0, ≤ `MAX_FILE_SIZE`), validate filename, sniff MIME từ `head`, tạo key, ghi `pending_uploads`, ký URL.
- Giữ nguyên quy tắc `resolveUploadContent` (đuôi + magic bytes phải khớp), chỉ đổi nguồn dữ liệu từ stream sang JSON.
- `PresignUploads` (batch) làm tương tự, mỗi item có `size` + `head_base64`.
- Bỏ multipart khỏi API presign.

### 3.2 Ký Content-Length (sửa P2)
- `PresignedURL` nhận thêm `size` và đưa `ContentLength` vào `PutObjectInput` khi ký. Client phải gửi đúng header `Content-Length`, S3 từ chối nếu khác.
- Kiểm tra thực tế SeaweedFS có chấp nhận chữ ký có `content-length` không (test trước khi làm hẳn).

### 3.3 Bảng `pending_uploads` (sửa P4, hỗ trợ P5)
- Cột: `key` (unique), `user_id`, `size`, `mime`, `created_at`, `expires_at`, `consumed_at`.
- Presign: ghi 1 dòng.
- `create_article` với `storage_key`:
  1. Tìm `pending_uploads` theo `key` và `user_id` của request. Không có thì 400.
  2. `HeadObject`: size phải khớp `size` đã khai.
  3. Đọc 512 byte đầu (GET với Range) và sniff lại. Không khớp thì 400, đánh dấu từ chối.
  4. Đánh dấu `consumed_at` trong cùng transaction với tạo bài.
- Dòng quá `expires_at` và chưa consumed là ứng viên dọn.

### 3.4 Dọn blob mồ côi (P5)
- Lifecycle rule trên prefix `uploads/` (nếu SeaweedFS hỗ trợ), hoặc job định kỳ: xóa object có `pending_uploads` đã hết hạn và chưa consumed.
- Thời hạn mặc định: 2 ngày (tách khỏi `PRESIGN_EXPIRY` 15 phút).

## 4. Thay đổi code dự kiến

| File | Thay đổi |
|---|---|
| `src/controller/dtos/file_dto.go` | Request presign dạng JSON (`filename`, `size`, `head_base64`) |
| `src/controller/apis/file_controller.go` | Bỏ đọc multipart, bind JSON |
| `src/controller/services/file_service.go` | Nhận size + head từ DTO, ghi `pending_uploads`, ký có `size` |
| `src/infrastructure/seaweedfs/storage.go` | `PresignedURL` ký `ContentLength` |
| `src/domain/` + `src/infrastructure/repository/` | Model + repo `pending_uploads` |
| `src/infrastructure/postgres/migrate.go` | AutoMigrate bảng mới + index `(key)` unique |
| `src/controller/services/articles_service.go` | Kiểm tra `pending_uploads`, size, sniff lại, consume trong transaction |
| `src/controller/services/` (job mới) | Dọn `pending_uploads` và object hết hạn |
| `src/loader/loader.go`, `worker.go` | Wiring repo + job |
| `loadtest/upload_sim.py` | Gọi presign kiểu JSON, gửi `head_base64`, gửi đúng `Content-Length` |

Test:
- Unit: sniff/size/ownership (từng case P1–P4, kể cả key của user khác và size sai).
- Integration với SeaweedFS thật: PUT size đúng (pass), PUT size sai (bị S3 từ chối), PUT nội dung khác loại (bị chặn khi tạo bài).
- Load: chạy lại `upload_sim.py` với 10 user × 10 file. Đo presign (giờ chỉ là JSON nhỏ), PUT, và RAM API khi upload song song.

## 5. Thứ tự thực hiện

1. ✅ **Spike `Content-Length` (xong, 2026-10-19).** Presign với `ContentLength=10`, `SignedHeaders` gồm `content-length`. Kết quả với SeaweedFS thật:
   - PUT đúng 10 byte: 200.
   - PUT 20 byte (kể cả khai `Content-Length: 20`): 403 `SignatureDoesNotMatch`.
   - PUT 5 byte: 403 `SignatureDoesNotMatch`.

   Kết luận: SeaweedFS **có** khoá size qua chữ ký. Hướng 3.2 khả thi. File spike đã xoá.
2. Thêm bảng `pending_uploads` + repo + migrate.
3. Đổi presign sang JSON + ký `ContentLength` + ghi `pending_uploads`.
4. Sửa `create_article` (ownership, size, sniff lại, consume).
5. Viết test (unit + integration).
6. Sửa `upload_sim.py`, chạy lại kịch bản tải.
7. Job dọn blob mồ côi + lifecycle.
8. `go build`, `go vet`, `go test ./...`, `golangci-lint run ./...`.

## 6. Tiêu chí hoàn thành

- Presign không nhận file: request presign có body < 1KB ở mọi kích thước file.
- Client không PUT được file khác `size` đã khai.
- Không tạo được bài từ key không thuộc user hiện tại.
- Nội dung không khớp loại file bị chặn trước khi extract.
- Blob không được consume bị dọn sau hạn.
- Toàn bộ build/vet/test/lint xanh.

## 7. Ngoài phạm vi (làm sau)

- Thay thư viện PDF để RAM worker không phụ thuộc kích thước file (mục 2, "ngoài phạm vi").
- Upload multipart cho file > 5GB (hiện `MAX_FILE_SIZE` = 500MB, không cần).
- Chạy nhiều replica worker và test lease.
