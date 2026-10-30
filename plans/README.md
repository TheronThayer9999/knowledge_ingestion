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
| 23 | 2026-10-26 | Chat FE: `[^n]` thành chip bấm được, gập NGUỒN TRÍCH DẪN, hết đè icon tune | [file](archive/2026-10/2026-10-26-dot-23-chat-fe-footnote-va-nguon-gap.md) | `85ede27` |
| 24 | 2026-10-26 | Session chat lưu DB — S1 + S4 theo `RAG_RESEARCH.md` §6.7 (backend) | [file](archive/2026-10/2026-10-26-dot-24-session-chat-db-s1-s4.md) | `b6df004` |
| 25 | 2026-10-26 | Trả lời như chuyên gia (mục 10 prompt) + eval đa provider theo lô 9router/nvidia/openrouter (trần 40 req/phút) + UI/UX phiên chat ở FE (sidebar, resume, đổi tên/xoá) + data phức tạp (Excel multi-sheet) + tài liệu demo | [file](archive/2026-10/2026-10-26-dot-25-tra-loi-nhu-chuyen-gia-va-eval-da-provider.md) | `cb21484` |
| 27 | 2026-10-27 | Sidebar quản lý phiên (nhóm 1–9) + hiệu ứng stream đáp án ở FE (không đổi API/BE) | [file](archive/2026-10/2026-10-27-dot-27-sidebar-phien-va-hieu-ung-stream.md) | `7838e0f` |
| 28 | 2026-10-28 | Provider/model lên UI: `/llm/options` (nấc 1) → popup key CSDL + tải model (nấc 2) → tạo/sửa/tắt/xoá ĐỊNH NGHĨA provider từ UI, config chỉ còn là mặc định (nấc 3) + nhóm model theo provider và khoá "Suy luận sâu" theo năng lực model; phụ: nút đính kèm tệp ở màn chat | [file](archive/2026-10/2026-10-28-dot-28-llm-options-va-model-picker.md) | `eb3649a` (BE) + `eea1373`/`192f6a9` (FE) |

Ghi chú: mốc **Ngày** lấy theo tiền tố tên file (ngày viết kế hoạch) — Đợt 3..9 mang
`2026-10-27` dù Đợt 10..22 lại là `2026-10-26`, nên đừng tin thứ tự tên file, hãy xếp
theo số Đợt.

## Đang chạy

| Đợt | Ngày | Nội dung | Kế hoạch |
|---|---|---|---|
| 26 | 2026-10-27 | Chart dựng từ chunk đã dẫn, LLM chọn loại biểu đồ, server dựng số | [kế hoạch](2026-10-27-dot-26-chart-tu-chunk-da-dan-va-llm-chon-loai.md) |
| 31 | 2026-10-30 | OCR tài liệu scan → **PDF**: tab Quét tách **4 tab** (Tài liệu / Xem & sửa / PDF chuẩn / Lịch sử), job OCR async **claim theo trang**, kết quả = PDF (nguồn PDF trả nguyên bản, ảnh bọc một trang); **đã bỏ lớp chữ ẩn, bỏ `/scan/ocr` đồng bộ, không nhận WEBP**; kèm janitor TTL + semaphore render; tham chiếu quy trình `D:\code\work\autoai` | [kế hoạch](2026-10-30-dot-31-ocr-searchable-pdf-4-tab.md) |

## Còn chờ (đã ghi backlog, chưa có file kế hoạch)

retry bài failed từ UI · hết `panic("implement me")` trong `UserRepository` · prompt biết
scope · QoS (semaphore/429/metrics) · outbox-RabbitMQ và VLM · trang tìm kiếm FE · Auth P1
(refresh cookie HttpOnly + CSRF + UI phiên).

Sau Đợt 24 còn các nhánh của riêng session, xếp theo đúng thứ tự §6.7 đã chốt (phần UI của S1+S4 đã gộp vào Đợt 25):
S3 ngân sách tool-result theo lượt → S2 nén luỹ tiến → S5 regenerate/fork → S6 xuất
hội thoại.
