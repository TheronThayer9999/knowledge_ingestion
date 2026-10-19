# Kế hoạch: trích text PDF theo luồng (RAM không phụ thuộc kích thước file)

Trạng thái: **đang thực hiện**.
Ngày lập: 2026-10-19.

## 1. Vấn đề

`src/common/extractor/pdf.go` dùng `gopdf` nên:
- `io.ReadAll` toàn bộ PDF vào `[]byte` (tối đa 100MB).
- `gopdf.OpenBytes` phân tích cả file trong RAM.
- Đo thật trên `dotnet-fundamentals.pdf` (89.8MB, 3629 trang): RAM đỉnh **~920MB**.

Yêu cầu của dự án: RAM không được tỉ lệ với kích thước file.

## 2. Đo so sánh (cùng file, cùng máy)

| Cách | Thời gian | RAM đỉnh | Số ký tự |
|---|---|---|---|
| gopdf (hiện tại) | chưa đo riêng | ~920MB | 5.27M (đã lưu DB, sau trim) |
| `pdftotext -enc UTF-8` (poppler) | 14.2s | 79MB | 5.35M (raw) |

Không có NUL trong đầu ra pdftotext. Poppler đã có sẵn trên máy (cùng bộ với `pdftoppm` dùng cho OCR).

## 3. Thiết kế

1. Ghi stream blob từ S3 xuống file tạm (`io.Copy`, RAM hằng số), đếm byte, vẫn giữ trần `maxPDFBytes`.
2. Lấy số trang bằng `pdfinfo` (dòng `Pages:`).
3. Trích theo khoảng trang bằng `pdftotext -f N -l M -enc UTF-8 file.pdf -` (stdout), mỗi khoảng 200 trang. Đọc stdout theo dòng, tách trang tại ký tự `\f`, rồi `visit` ngay. Mỗi lần gọi chỉ giữ 1 trang text trong RAM.
4. Trang rỗng (scan) vẫn đi qua nhánh OCR hiện có (`pdftoppm` render + tesseract). Không đổi.
5. `fromPage` vẫn là điểm resume: chỉ gọi pdftotext từ trang đó.
6. Đường dẫn `pdftotext`/`pdfinfo`: suy từ thư mục của `ocr.render_binary` (cùng gói poppler). Nếu thiếu `render_binary` thì báo lỗi rõ ràng thay vì âm thầm bỏ qua.

## 4. Thay đổi code

| File | Thay đổi |
|---|---|
| `src/common/extractor/pdf.go` | Thay gopdf bằng spool file + pdfinfo + pdftotext theo khoảng trang |
| `src/common/extractor/pdf_test.go` (hoặc `extractor_test`) | Test trích đúng số trang, đúng nội dung, resume từ `fromPage`, trang trống |
| `go.mod` / `go.sum` | Bỏ `gopdf` nếu không còn import (sau `go mod tidy`); giữ nếu `despace_parity_test` còn dùng |
| `configs/config.json` | Không đổi (suy từ `render_binary`) |

## 5. Tiêu chí hoàn thành

- RAM worker khi trích `dotnet-fundamentals.pdf` không vượt ~150MB (đo bằng sampler như lần trước).
- Số trang và tổng ký tự khớp với pdftotext đo ở mục 2 (sai lệch trong khoảng do trim).
- Không có NUL trong text đã lưu.
- Resume từ `fromPage` không trùng trang.
- `go build`, `go vet`, `go test ./...`, `golangci-lint run ./...` xanh.

## 6. Rủi ro và việc cần xác nhận

- **Môi trường production phải có poppler-utils** (`pdftotext`, `pdfinfo`, `pdftoppm`). Đây là yêu cầu mới ngoài gopdf. Cần ghi vào Dockerfile của worker.
- Chất lượng text khác gopdf (gopdf dựng khoảng cách glyph, pdftotext không). Cần kiểm tra trên vài file thật sau khi đổi, đặc biệt tiếng Việt có dấu.
- Trần `maxPDFBytes` 100MB vẫn là trần từ chối, không cắt nội dung. Với đường spool-ra-đĩa, trần này không còn là giới hạn RAM. Có nên tăng hay bỏ không là quyết định riêng.

## 7. Phương án A: song song theo khoảng trang (trong 1 worker)

Trạng thái: **đã làm xong, đã đo** (2026-10-19).

Kết quả đo trên `dotnet-fundamentals.pdf` (3629 trang):

| | Tuần tự 1 lần pdftotext | Song song (4 khoảng) |
|---|---|---|
| Thời gian | 14.2s | **3.9s** |
| RAM tiến trình cha (đỉnh) | 79MB | 53MB (không tính các tiến trình pdftotext con) |
| Tổng ký tự (sau khi bỏ khoảng trắng thừa) | 5.35M (raw) | 5.16M |

Lưu ý: số ký tự khác gopdf cũ (5.27M đã lưu DB) khoảng 2%. Chưa xác định bên nào giữ đủ hơn; cần kiểm tra tay vài trang tiếng Việt có dấu trước khi kết luận.

Các thay đổi: `extractor.ExtractRawPages` nhận `have map[int]bool` thay cho `fromPage`; `extractPDFPages` chia khoảng, chạy pool `constants.EXTRACT_PAGE_WORKERS` (= 4); `ExtractAll` sắp theo `Seq`; service `article_extract_service.go` truyền tập `seen`. Test mới: bỏ qua trang đã có, visit không đồng thời, lỗi dừng sớm, `pendingPDFRanges`. `go test -race` cho extractor và services pass.

- Spool file 1 lần, mọi luồng cùng đọc file đó (pdftotext mở chỉ đọc).
- `pdfinfo` lấy số trang 1 lần. Các trang chưa có (theo `seq`) được gom thành khoảng liên tiếp, mỗi khoảng tối đa `pdfTextChunkPages` trang.
- Một pool goroutine `EXTRACT_PAGE_WORKERS` (constants, mặc định 4) xử lý các khoảng. Mỗi luồng chạy `pdftotext` (và OCR nếu trang scan).
- `visit` được gọi dưới mutex (hợp đồng cũ: không đồng thời), nên service không đổi cách ghi. Thứ tự ghi không quan trọng vì `(article_id, seq)` là khoá và phase index đọc theo `seq`.
- Lỗi một khoảng: huỷ các khoảng còn lại, trả lỗi đầu tiên. Phần đã ghi được giữ, lần sau resume theo `seq` còn thiếu.
- Resume: thay `fromPage int` bằng tập trang đã có (`have map[int]bool`). Không còn giả định trang liên tục từ đầu.
- Tổng số tiến trình `pdftotext`/OCR đồng thời = `EXTRACT_PAGE_WORKERS` (vì `EXTRACT_POOL_SIZE` = 1).
- PDF text/docx/txt không đổi cách xử lý; `have` chỉ dùng cho PDF, các loại khác vẫn dựa vào dedupe trong visit.
