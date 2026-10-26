# plans/ — sổ kế hoạch theo từng đợt (Đợt N)

Quy trình đã thống nhất: **ghi kế hoạch `.md` trước khi sửa code**, mỗi đợt một file
`<ngày>-dot-<N>-<slug>.md` đặt ở thư mục gốc này; khi đợt xong thì bổ sung mục
`## Kết quả` (việc đã làm, cách kiểm chứng, bằng chứng E2E, sai sót gặp phải) rồi
chuyển file xuống `archive/<YYYY-MM>/`. Folder gốc vì thế chỉ chứa kế hoạch **đang
chạy**, còn quá khứ nằm ở archive — xem bảng dưới đây để biết đợt nào ở đâu.

Cột **chốt** là commit chứa mục `## Kết quả` của đợt đó (thường chính là commit code
của đợt); muốn đọc lại nguyên văn kế hoạch cũ thì `git show <hash>`, hoặc mở file
trong `archive/`.

## Đã hoàn tất — archive/2026-10/

| Đợt | Ngày | Nội dung | Kế hoạch | Chốt |
|---|---|---|---|---|
| 1 | 2026-10-25 | Đối chiếu tất định hai bản trích (X1) | [file](archive/2026-10/2026-10-25-dot-1-doi-chieu-tat-dinh.md) | `b429a85` |
| 2 | 2026-10-25 | VLM reader theo OOP/SOLID | [file](archive/2026-10/2026-10-25-dot-2-vlm-reader.md) | `ef2950f` |
| 3 | 2026-10-27 | Detector trang phức tạp (L1) | [file](archive/2026-10/2026-10-27-dot-3-detector-trang-phuc-tap.md) | `b3b2de8` |
| 4 | 2026-10-27 | OCR song ngữ `vie+eng` + vá tiêu chí gọi VLM | [file](archive/2026-10/2026-10-27-dot-4-ocr-vie-eng.md) | `aee78e7` |
| 5 | 2026-10-27 | Tool báo trang PDF không đọc được (bỏ VLM khỏi pipeline) | [file](archive/2026-10/2026-10-27-dot-5-tool-trang-khong-doc-duoc.md) | `6ad5910` |
| 6 | 2026-10-27 | Trích `.xlsx`/`.pptx` + chặn `.zip` (đồng bộ whitelist upload ↔ extractor) | [file](archive/2026-10/2026-10-27-dot-6-office-extraction.md) | `21bfbf3` |
| 7 | 2026-10-27 | Reader là "tool" (port + registry) + 2 tool agent cho Excel | [file](archive/2026-10/2026-10-27-dot-7-reader-port-mcp.md) | `027749c` |
| 7b | 2026-10-27 | Để LLM thực sự gọi `list_sheets`/`read_sheet` khi cần dữ liệu bảng | [file](archive/2026-10/2026-10-27-dot-7b-llm-biet-goi-tool-sheet.md) | `216093b` |
| 8 | 2026-10-27 | Chọn provider/model từ CLI + prompt bảng tính nạp theo điều kiện | [file](archive/2026-10/2026-10-27-dot-8-cli-llm-va-prompt-dong.md) | `61dbea4` |
| 9 | 2026-10-27 | Ngân sách hàm cho pipeline (hàm không quá to, không vụn) | [file](archive/2026-10/2026-10-27-dot-9-ngan-sach-ham-va-tach-stage.md) | `6d21584` |
| 10 | 2026-10-26 | Upload nhiều file một lượt + log theo file/stage | [file](archive/2026-10/2026-10-26-dot-10-upload-nhieu-file-va-log-theo-file.md) | `5de10dd` |
| 11 | 2026-10-26 | Rõ ranh giới "gọi model" theo từng phase | [file](archive/2026-10/2026-10-26-dot-11-ro-ranh-gioi-embed-va-llm-theo-phase.md) | `30d8c36` |
| 12 | 2026-10-26 | Provider NVIDIA cho BE + test API thực tế | [file](archive/2026-10/2026-10-26-dot-12-nvidia-provider-va-test-api-thuc-te.md) | `69a2008` |
| 13 | 2026-10-26 | Xác minh "UI báo xử lý xong mà vẫn gọi LLM" (provider 9router) | [file](archive/2026-10/2026-10-26-dot-13-xac-minh-call-llm-sau-khi-xu-ly-xong.md) | `e792fcd` |
| 14 | 2026-10-26 | Lỗi LLM / client ngắt không còn văng thành "lỗi runtime 500" | [file](archive/2026-10/2026-10-26-dot-14-loi-llm-khong-vang-500.md) | `93bd280` |
| 15 | 2026-10-26 | Log rõ đang xử lý FILE nào (API + worker) | [file](archive/2026-10/2026-10-26-dot-15-log-dang-xu-ly-file-nao.md) | `7963bab` |
| 16 | 2026-10-26 | Hiện định danh file trên danh sách tài liệu + tìm theo file | [file](archive/2026-10/2026-10-26-dot-16-hien-file-tren-danh-sach-va-tim-theo-file.md) | `4003e74` |
| 17 | 2026-10-26 | Bỏ key thật khỏi `configs/config.json` (file được track) | [file](archive/2026-10/2026-10-26-dot-17-bo-key-that-khoi-config-dang-track.md) | `216693a` |
| 18 | 2026-10-26 | Log MỌI lời gọi LLM (kèm phase) + bằng chứng worker đã hết việc | [file](archive/2026-10/2026-10-26-dot-18-log-moi-loi-goi-llm.md) | `27fc50f` |
| 19 | 2026-10-26 | Giữ nguyên tên file khi upload | [file](archive/2026-10/2026-10-26-dot-19-giu-ten-file-khi-upload.md) | `a6a263c` |
| 20 | 2026-10-26 | Nút "Tải xuống" cho tài liệu (presigned GET) | [file](archive/2026-10/2026-10-26-dot-20-nut-tai-xuong.md) | `2252446` |
| 21 | 2026-10-26 | Log có trật tự: field order + cắt giá trị dài + ẩn query link | [file](archive/2026-10/2026-10-26-dot-21-log-co-trat-tu.md) | `41396ec` |
| 22 | 2026-10-26 | BE chuẩn hoá trích dẫn `[^n]`, không để lộ ID kỹ thuật | [file](archive/2026-10/2026-10-26-dot-22-chuan-hoa-trich-dan.md) | `6235565` |

Ghi chú: mốc **Ngày** lấy theo tiền tố tên file (ngày viết kế hoạch) — Đợt 3..9 mang
`2026-10-27` dù Đợt 10..22 lại là `2026-10-26`, nên đừng tin thứ tự tên file, hãy xếp
theo số Đợt.

## Còn chờ (đã ghi backlog, chưa có file kế hoạch)

Đợt 23 chat FE (footnote bấm được, đóng/mở NGUỒN TRÍCH DẪN, sửa chồng icon tune) ·
Đợt 24 lịch sử chat lưu DB + sidebar · Đợt 25 retry bài failed từ UI · Đợt 26 hết
`panic("implement me")` trong `UserRepository` · Đợt 27 prompt biết scope · Đợt 28
QoS (semaphore/429/metrics) · Đợt 29 outbox-RabbitMQ và VLM · Đợt 30 trang tìm kiếm
FE · Đợt 31 Auth P1 (refresh cookie HttpOnly + CSRF + UI phiên).
