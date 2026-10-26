# Phiên chat + dữ liệu phức tạp: hành vi, test case, kịch bản demo (Đợt 25)

Tài liệu dành cho người demo. Mọi số liệu "đáp án đúng" dưới đây lấy trực tiếp
từ fixture ở `loadtest/fixtures_complex/` (sinh bằng
`python loadtest/make_complex_fixtures.py`), không phải số tưởng tượng — câu nào
trả lời khác số trong bảng này là lỗi hệ thống, không phải lỗi đề.

## 0. Chuẩn bị

| Bước | Lệnh / thao tác |
|---|---|
| Hạ tầng | Docker: postgres, qdrant, seaweedfs-s3, rabbitmq |
| Config | `configs/config.local.json` — providers `9router` / `nvidia` / `openrouter`; `agent.rate_limit_rpm` đang đặt **30** (trần provider 40 req/phút, để thấp hơn làm sàn tự vệ) |
| Sinh fixture | `python loadtest/make_complex_fixtures.py` → `loadtest/fixtures_complex/` (5 xlsx + 3 pdf) |
| Chạy backend | `go run ./cmd/apis` (cổng 9001) và `go run ./cmd/worker` (2 phase: extract → index) |
| Chạy FE | `cd knowledge_ingestion_fe` → `npm start` |
| Upload | Trang Documents, upload **từng file** trong `fixtures_complex` vào **cùng một danh mục** để một phiên chat scope hẹp đọc được hết |
| Chờ index | `SELECT id, name, extract_status, index_status, index_done_chunks, page_count FROM articles ORDER BY id DESC LIMIT 12;` — tất cả `done` mới bắt đầu hỏi |
| Kiểm tra DB | `docker exec -i theron_postgres psql -U theron_user -d manager_db` |

## 1. Hành vi đã cài (input → UI → DB)

| Tình huống | UI | DB |
|---|---|---|
| Mở trang chat | sidebar trái: scope + thinking + nút "Cuộc trò chuyện mới" + danh sách phiên của user; transcript trống, chưa gọi POST nào | 0 row trong `conversations` |
| Gõ câu đầu, bấm Gửi | FE tạo phiên **ngay trước lượt gửi** (vì `/chat` không trả `conversation_id`), rồi mới append bong bóng user | 1 row `conversations` (title rỗng) |
| Trả lời thành công | tiêu đề phiên = câu hỏi đầu (auto-title), meta "1 lượt"; refresh sidebar sau lượt đầu | +2 row `messages` (role `user` rồi `assistant`, thứ tự theo `id`); response `message_count = 2` — con số này **đếm bằng GROUP BY**, không phải cột lưu trên phiên |
| Trả lời lỗi / timeout | toast, nháp được khôi phục, bong bóng user bị bỏ | **không có row nào** trong `messages` cho lượt đó |
| Bấm Dừng giữa lúc streaming | trả lời một nửa biến mất, ô nhập mở lại | **không có row nào** (half turn chủ ý bỏ) |
| Chọn phiên khác | nạp transcript 50 lượt gần nhất theo thứ tự thời gian; restore thinking + scope | `updated_at` được chạm |
| Đã có lượt trong phiên | select scope + thinking **khoá**, hiện dòng giải thích | — |
| Đổi tên | input inline, Enter lưu, Esc huỷ; không đổi thì không gọi PATCH | `PATCH /conversations/:id` |
| Xoá | confirm dialog → phiên mất khỏi danh sách, về phiên mới | `deleted_at NOT NULL` (xoá MỀM, message giữ lại) |
| >20 phiên | nút "Tải thêm" (gửi con trỏ `before` = id phiên cuối; server dịch ra cặp `(last_message_at, id)`) | — |
| F5 | FE **không** nhớ phiên đang mở → trang về phiên mới (giới hạn đã ghi ở mục 8) | — |

## 2. Test case phiên chat (S-xx)

| ID | Cách làm | Mong đợi |
|---|---|---|
| S-01 | Mở trang, không hỏi gì | `SELECT count(*) FROM conversations;` không tăng — phiên chỉ tạo khi thật sự hỏi |
| S-02 | Hỏi 1 câu | 1 phiên, tiêu đề = câu hỏi, meta "1 lượt"; DB: 1 row `conversations` + 2 row `messages` |
| S-03 | Hỏi tiếp câu 2 trong cùng phiên | Tiêu đề **không đổi**; `message_count = 4`, 2 row `messages` id kế tiếp |
| S-04 | DevTools → Network → `/chat` | body có `conversation_id`, **không có** key `history` (server là nguồn lịch sử duy nhất) |
| S-05 | Tắt worker hoặc sai key provider rồi hỏi | toast lỗi, nháp cũ còn nguyên trong ô nhập, không có message mới trong DB |
| S-06 | Bấm Dừng giữa câu trả lời | UI mất trả lời dở; DB không có row lượt đó; hỏi tiếp vẫn bình thường |
| S-07 | Click phiên cũ trong sidebar | Transcript đúng thứ tự thời gian, số lượt khớp `message_count`; khối "suy nghĩ" **không** hiện (không lưu reasoning) |
| S-08 | Tạo phiên khi thinking đang bật, hỏi 1 câu, mở lại | toggle thinking tự bật lại theo phiên |
| S-09 | Chọn scope = 1 danh mục, hỏi, mở lại phiên | select trở về đúng danh mục đó |
| S-10 | Phiên đã có lượt | select scope + toggle thinking disabled, kèm dòng giải thích vì sao khoá |
| S-11 | Bấm biểu tượng sửa, đổi tên, Enter | tên đổi ngay; DB `title` đổi; `updated_at` đổi |
| S-12 | Bấm sửa, xoá hết chữ, Enter | **không** gửi PATCH (backend `binding:"required,min=1"` → 400); giữ tên cũ |
| S-13 | Bấm sửa, Esc | trở lại tiêu đề cũ, không gọi API |
| S-14 | Xoá phiên | confirm dialog → phiên biến mất; `deleted_at` có giá trị; row `messages` của nó **vẫn còn** (soft delete) |
| S-15 | Xoá phiên đang mở | UI về phiên mới trống, không crash |
| S-16 | Tạo >20 phiên (script) rồi bấm "Tải thêm" | fetch `?before=<id cuối>&limit=20`, nối vào cuối danh sách, không lặp bản ghi |
| S-17 | Hai tab: tab A xoá phiên, tab B click vào nó | toast + reset về phiên mới, sidebar reload; API trả 404 (không tồn tại với user này) |
| S-18 | Login user B, gọi `GET /conversations/<id của A>/messages` | 404 — mọi query gắn `user_id` vào WHERE (không truyền user id từ client); không leak nội dung |
| S-19 | Đăng xuất / hết hạn token rồi thao tác | FE về login, không để lại request treo |
| S-20 | Gửi câu hỏi đầu >60 ký tự | Backend cắt title theo **rune** ở `ClipTitle` (trần `TitleAutoClipRune = 60`, có dấu `…`), không bao giờ vượt `varchar(120)`; sidebar vẫn ellipsis bằng CSS khi đổi tên tay dài tối đa 120 |
| S-21 | Phiên có 60 lượt, mở lại | hiện 50 lượt gần nhất (giới hạn mục 8), câu hỏi mới vẫn được ngữ cảnh đúng |
| S-22 | Tạo phiên qua API với `thinking:false` trong khi FE đang bật | quy tắc merge: request để trống → **lấy giá trị của phiên**; FE vì thế khoá control ngay từ lượt đầu |

## 3. Test case dữ kiện & trích dẫn (X-xx)

### `bang_luong_q3.xlsx` (4 sheet khai trong workbook, 3 sheet có dữ liệu)

| ID | Câu hỏi | Đáp án đúng | Bẫy |
|---|---|---|---|
| X-01 | File này có mấy sheet? | 3 sheet có nội dung: `Luong`, `Phu_cap`, `Ghi_chu`; `So_lieu_cho_cap_nhat` **rỗng nên không vào nội dung trích xuất** | Trả lời "4" là đếm theo khai báo, không theo dữ liệu công cụ thấy |
| X-02 | Tổng thực nhận quý 3? | **29.970.000 VNĐ** (cột *Tổng thực nhận*, dòng *Tổng cộng*) | Cộng nhầm cột *Nhận ròng* = 27.346.500 |
| X-03 | Vì sao NV05 phụ cấp ăn trưa 600.000 còn NV01 800.000? | Ngưỡng **22 ngày công**: ≥22 → 800.000, <22 → 600.000; NV01 22,5 ngày, NV05 20 ngày + ghi chú "nghỉ 5 ngày trong quý" | Phải nối 2 sheet; trả lời "bảng không giải thích" là sai |
| X-04 | NV03 có được hưởng phụ cấp không? | **Hai nguồn mâu thuẫn**: `Luong` để trống ô Phụ cấp (0đ, thực nhận 3.870.000), `Phu_cap` ghi NV03 600.000/tháng | Tự chọn một trong hai, hoặc cộng số nhân ba tháng rồi khẳng định — phải nêu mâu thuẫn và đề nghị kế toán xác nhận |
| X-05 | Thuế TNCN áp tỷ lệ bao nhiêu? | Hai mức theo `Ghi_chu`: bậc 1 **10%**, bậc thấp **5%**; đối chiếu `Luong`: NV01/02/04 đúng 10%, NV03/05 đúng 5% | Nói "15%" chung chung, hoặc chỉ nêu một mức |
| X-06 | Hạn chốt lương? | Ngày 05 của tháng kế tiếp | — |
| X-07 | Bảng do ai lập, cập nhật khi nào? | Phòng Nhân sự, cập nhật 05/10/2025 | — |
| X-08 | Ngày lễ, Tết đi tính thế nào? | Hệ số 300% lương ngày | Ask "NV04 làm lễ được bao nhiêu" → **không có dữ liệu ngày lễ**, phải nói rõ thiếu |

### `hop_dong_thue_mat.xlsx`

| ID | Câu hỏi | Đáp án đúng | Bẫy |
|---|---|---|---|
| X-09 | HM-01 một tháng bao nhiêu tiền? | **130.350.000 VNĐ** = 48.750.000 (`Thue_Van_Phu`) + 81.600.000 (`Thue_Kho`) | Chỉ đọc sheet đầu → 48.750.000 |
| X-10 | Riêng khối văn phòng thu bao nhiêu? | 74.550.000 (HM-01 48.750.000 + HM-02 25.800.000) | Cộng cả sheet kho vào |
| X-11 | Kiểm tra cột Thành tiền có đúng công thức? | Đúng cả 4 dòng: 250×(180.000+15.000)=48.750.000; 120×215.000=25.800.000; 1200×68.000=81.600.000; 800×63.000=50.400.000 | Test 1 dòng rồi tuyên bố "đúng tất cả" |
| X-12 | Kỳ thanh toán? | Ngày 10 hằng tháng, chuyển khoản | — |

### `quy_che_phi_v1.xlsx` vs `quy_che_phi_v2.xlsx`

| ID | Câu hỏi | Đáp án đúng | Bẫy |
|---|---|---|---|
| X-13 | Phí công chứng hợp đồng hiện hành? | **150.000 VNĐ/hợp đồng** theo v2 (áp dụng từ 01/10/2025), nêu rõ v1 mức 100.000 đã bị bãi bỏ | Lấy 100.000 của v1 |
| X-14 | Dịch vụ nào mới ở bản 2? | PQ-04 Chứng thực chữ ký 12.000 VNĐ/chữ ký; PQ-03 tra cứu giữ nguyên 30.000 | Liệt kê cả PQ-01/PQ-02 là "dịch vụ mới" (chúng chỉ đổi mức) |
| X-15 | Phí cấp bản sao bao nhiêu? | **Không so sánh trực tiếp được**: xlsx v2 tính 8.000đ/**bản/lần**, PDF quy định v3.0 tính 3.000đ/**trang** (10 trang đầu) | Trộn hai đơn vị tính thành "tăng/giảm" |

### `quy_dinh_nhieu_phu_luc.pdf` (3 trang: quy định + Phụ lục A + Phụ lục B)

| ID | Câu hỏi | Đáp án đúng | Bẫy |
|---|---|---|---|
| X-16 | Sao 25 trang hết bao nhiêu? | 60.000 VNĐ (10×3.000 + 15×2.000) — file có sẵn ví dụ này | Nhân 25×3.000 = 75.000 |
| X-17 | Sao 30 trang? | **70.000 VNĐ** = 10×3.000 + 20×2.000 (phải tự suy ra từ Phụ lục A) | Copy ví dụ 25 trang trong file mà không tính lại |
| X-18 | Ai được miễn phí, giới hạn ra sao? | 3 nhóm ở Phụ lục B; **mỗi loại giấy tờ không quá 02 bản** (Điều 3.2); **không miễn** bản sao phục vụ giao dịch dân sự từ 50.000.000 VNĐ trở lên | Chỉ kể 3 nhóm, bỏ Điều 3.2 và ngoại lệ |
| X-19 | Mất bao lâu để nhận kết quả? | 03 ngày làm việc kể từ ngày nhận đủ hồ sơ (Điều 4.2) | Quên điều kiện "nhận đủ hồ sơ" |
| X-20 | Quy định này còn hiệu lực không? | v3.0 hiệu lực từ 01/10/2025, thay thế v2.0 (Điều 5); đối chiếu ngày hôm nay trong prompt | Trả lời "có" mà không nêu căn cứ hiệu lực |

### `bang_ke_toan_multi_sheet.xlsx`

| ID | Câu hỏi | Đáp án đúng | Bẫy |
|---|---|---|---|
| X-21 | Tổng chi trong kỳ? | **58.250.000 VNĐ** (cột Nợ, theo quy ước sheet `Quy_uoc`: Nợ = chi tiền) | Không đọc quy ước → đảo Nợ/Có, trả 103.400.000 |
| X-22 | Cộng lại từng chứng từ chi, có khớp dòng "Cộng phát sinh"? | Khớp: 4.200.000+11.500.000+6.800.000+2.300.000+18.000.000+5.400.000+9.600.000+450.000 = 58.250.000 | Nói "tin theo dòng Cộng" mà không kiểm |
| X-23 | Số dư cuối kỳ theo sổ? | 45.150.000 VNĐ (Có − Nợ) | Lấy 103.400.000 − 58.250.000 rồi nhầm dấu |
| X-24 | Vì sao sổ lệch ngân hàng, lệch bao nhiêu? | Chênh **27.800.000 VNĐ** (sổ 45.150.000 vs ngân hàng 17.350.000), đúng bằng CT-019 thu khách hàng C chưa ghi nhận | Trả "chênh 27.800.000" mà không chỉ ra chứng từ |
| X-25 | Tháng 10 thu bao nhiêu? | 103.400.000 VNĐ (3 chứng từ thu: 30.000.000 + 45.600.000 + 27.800.000) | Bỏ CT-019 vì nó là nguyên nhân lệch |

### PDF scan + markdown/format

| ID | Câu hỏi | Mong đợi | Bẫy |
|---|---|---|---|
| X-26 | `scan_vua_text_vua_anh.pdf`: "trang 2 nói gì?" | OCR ra nội dung trang 2 (ảnh), trích dẫn đúng `page=2` | Nói "chỉ đọc được trang 1" |
| X-27 | "File này mấy trang, ai ban hành?" | Dùng công cụ thông tin tài liệu, không đoán | Tự đếm số đoạn trích |
| X-28 | "Lập bảng so sánh phí v1 và v2, 3 cột" | Markdown **bảng** render đúng, chỉ 3 dòng dịch vụ có thật, đủ cả đơn vị tính | Bịa thêm PQ-05 cho "đẹp bảng" |
| X-29 | "Trả lời trong 2 dòng: phí tra cứu hồ sơ?" | 30.000 VNĐ/lần, cả v1 và v2 | Viết thành đoạn 6 dòng + tiêu đề |

## 4. Test case chất lượng trả lời (Q-xx) — chấm theo `expert_answer_rubric.md`

| ID | Lượt hỏi | Mong đợi |
|---|---|---|
| Q-01 | A: "NV01 nhận ròng bao nhiêu?" → B: "Còn NV04 thì sao?" | B viết lại câu hỏi đầy đủ trước khi search, **nêu lại fact của A** (4.770.000) rồi trả lời 9.000.000 |
| Q-02 | "Bảng lương kỳ nào?" → "Cập nhật khi nào?" | Lượt 2 hiểu "nó" là bảng lương, đáp 05/10/2025, không search lại từ đầu vô ích |
| Q-03 | "Phí cấp bản sao bao nhiêu?" (2 nguồn) | Nêu cả hai, chỉ ra khác đơn vị tính, chốt theo phạm vi từng tài liệu |
| Q-04 | "Nhân viên được hỗ trợ tiền công tác bao nhiêu?" | "Không tìm thấy trong tài liệu đã upload", gợi ý từ khoá/kiểm tra file — **không** lấy mức thông lệ chung |
| Q-05 | "Thời tiết hôm nay thế nào?" / "Viết bài về AI" | Ngoài phạm vi tài liệu: từ chối nhẹ, mời hỏi về tài liệu; không bịa căn cứ |
| Q-06 | "HM-01 là ai?" | sheet `Ghi_chu` nói rõ cùng mã là **một khách hàng, hai mặt bằng** → trả lời đúng nghiệp vụ, không "không tìm thấy" |
| Q-07 | Hỏi lại đúng câu đã trả lời | Nhắc kết luận 1–2 câu + căn cứ cũ, chỉ bổ sung phần mới |
| Q-08 | Câu có điều kiện: "Nghỉ 5 ngày có bị cắt phụ cấp không?" | Nêu điều kiện 22 ngày công + ghi chú NV05 trước khi chốt, không trả lời "có/không" cụt |

## 5. Chạy đánh giá hàng loạt (xoay 3 provider, tôn trọng trần 40 req/phút)

```powershell
# 1) Selftest bộ chấm — chạy local, không gọi LLM, phải ra đủ 14/14
python loadtest/expert_eval.py --selftest

# 2) Smoke 1 cửa sổ rate limit (4 câu), xoay provider
python loadtest/expert_eval.py --providers 9router nvidia openrouter --batch-size 4 --max 4

# 3) Batch thật trên 29 case dữ liệu phức tạp: tự upload fixture, tự chờ index
#    (lệnh đã chạy thật ngày 2026-10-27 — 7 file, 6 câu đầu tiên)
python loadtest/expert_eval.py `
  --file docs_test_case_and_data/testcase/test_case_complex_data.md `
  --providers 9router nvidia openrouter --batch-size 4 --rpm 30 --concurrency 2 --max 6 `
  --docs loadtest/fixtures_complex/bang_luong_q3.xlsx loadtest/fixtures_complex/hop_dong_thue_mat.xlsx `
         loadtest/fixtures_complex/bang_ke_toan_multi_sheet.xlsx loadtest/fixtures_complex/quy_che_phi_v1.xlsx `
         loadtest/fixtures_complex/quy_che_phi_v2.xlsx loadtest/fixtures_complex/quy_dinh_nhieu_phu_luc.pdf `
         loadtest/fixtures_complex/scan_vua_text_vua_anh.pdf `
  --out loadtest/expert_eval_complex_results.json

# 4) Chạy tiếp với user đã upload (bỏ qua upload, trỏ vào danh mục cũ)
python loadtest/expert_eval.py --skip-upload --category-id <id> --providers nvidia --batch-size 4
```

Vì sao `--batch-size 4`: một lượt `/chat` là 8–10 lần gọi LLM nội bộ (đo ở Đợt
24), 4 câu ≈ 32–40 lần gọi, vừa khít trần 40 req/phút. Muốn nhanh hơn thì tăng
`--concurrency` (đè song song thời gian chờ), **không** tăng batch.

Test chậm thì thêm cờ `--chat-timeout` (mặc định 300s) và `--wait-index` (900s).
Kết quả thô: `l0` (kỹ thuật), `l1` (đúng số + tin cậy số), `l2` (hình thức, kèm
tên tiêu chí trượt) — phần `l2.failed` chỉ thẳng câu nào trượt tiêu chí nào.
Báo cáo JSON lưu nguyên `cites` của từng lượt, nên sửa rubric xong chấm lại được
offline bằng `grade()` mà không gọi lại LLM.

## 6. Kịch bản demo 10 phút

| Phút | Nói / làm |
|---|---|
| 0:00 | "Đây là trang chat, bên trái là danh sách cuộc trò chuyện và phạm vi tài liệu." — mở `/app/chat`, chưa hỏi gì (chưa có phiên nào) |
| 0:30 | Chọn scope = danh mục Kế toán–Nhân sự, bật "Suy nghĩ sâu" |
| 1:00 | Hỏi **X-03** (NV05 vs NV01). Chỉ vào: câu trả lời mở đầu bằng nguyên nhân, không phải "tôi sẽ tra cứu"; chip `[1] [3]` bấm ra tài liệu |
| 2:00 | "Phiên vừa rồi được lưu." — sidebar hiện tiêu đề = câu hỏi, meta "1 lượt" |
| 2:30 | Hỏi tiếp **X-04** (NV03 mâu thuẫn hai sheet). Nhấn mạnh: AI **tự báo mâu thuẫn dữ liệu** thay vì chọn bừa một số |
| 3:30 | **X-09** (HM-01 = 130.350.000). "Nó cộng xuyên hai sheet cùng mã hợp đồng, đây là chỗ người hay sót." |
| 4:30 | **X-24** (lệch ngân hàng 27.800.000 = CT-019). Cho thấy nó đọc *quy ước Nợ/Có* trước khi trả lời |
| 5:30 | **X-17** (30 trang = 70.000). "File chỉ có ví dụ 25 trang; nó tự áp công thức." |
| 6:30 | **X-13** (phí 150.000 theo v2, v1 đã bãi bỏ) rồi **X-15** (hai đơn vị tính khác nhau) — điểm bán: không nhầm phiên bản, không so sánh láo |
| 7:30 | Đổi tên phiên (S-11), mở lại phiên cũ (S-07): "lịch sử nằm ở server, tab khác đăng nhập vẫn thấy." |
| 8:30 | Gỡ: tắt worker → hỏi 1 câu (S-05) → "lỗi không để lại garbage trong history, nháp vẫn còn." |
| 9:00 | Xoá phiên (S-14) + nói rõ: "xoá mềm, audit vẫn giữ message." |
| 9:30 | "Còn thiếu gì": reasoning không lưu, F5 không nhớ phiên, 50 lượt gần nhất — nói trước để khỏi bị hỏi ngược |

## 7. Checklist UI tự test

Layout & theme
- [ ] Panel tiện ích nằm **trái** card chat trên desktop; nút gập ở góc trên-trái card, sát panel
- [ ] ≤1023px: panel thành thanh ngang **trên đầu**, nút gập nhảy sang góc trên-phải
- [ ] Danh sách phiên cao ~180px và cuộn được ở màn hẹp; không bóp nghẹt select/input
- [ ] Dark/light: màu nền phiên đang chọn, viền trái 2px vẫn rõ ở cả hai theme
- [ ] Tên phiên dài → ellipsis, không đẩy layout

Tương tác
- [ ] Click vào phiên = nội dung cũ hiện ra, scroll xuống cuối transcript
- [ ] Enter gửi, Shift+Enter xuống dòng; gõ tiếng Việt có dấu đang ghép mà bấm Enter **không** gửi nhầm (có guard `isComposing`)
- [ ] Ô nhập tự nâng chiều cao theo nội dung, tối đa 6 dòng rồi cuộn
- [ ] Đang gửi: textarea + nút Gửi khoá, nút Dừng hiện; bấm Dừng → hết khoá, nháp sạch
- [ ] Lỗi → toast, ô nhập nhận lại nội dung câu hỏi
- [ ] Rename: 2 lần Esc đều ổn (lúc đang gõ và lúc vừa bấm sửa)
- [ ] Delete: confirm dialog, Cancel không gọi API
- [ ] "Tải thêm" chỉ hiện khi trang trước trả đủ 20

Khả năng truy cập
- [ ] Tab đi qua: nút gập → select scope → toggle thinking → nút phiên mới → từng phiên → các nút sửa/xoá
- [ ] Phiên đang chọn có `aria-current="true"`; trạng thái khoá nói rõ bằng chữ, không chỉ bằng màu
- [ ] Mỗi nút icon có nhãn (title/aria-label) khi chỉ nhìn bằng screen reader
- [ ] Dòng "Đang tải…" là `role="status"` nên đọc được mà không cắt ngang

## 8. Giới hạn đã biết (nói trước khi demo)

1. **Reasoning không lưu** — mở lại phiên mất khối "suy nghĩ" (bảng `messages` không có cột reasoning).
2. **FE không nhớ phiên đang mở sau F5** — quay về phiên mới; lịch sử vẫn trong DB, chọn lại từ sidebar.
3. **50 lượt gần nhất** — transcript phân trang lùi về phía cũ chưa làm.
4. **Half turn không lưu** — dừng/lỗi giữa câu thì lượt đó không vào DB (chấp nhận có chủ ý).
5. **Xoá mềm** — message vẫn còn trong DB, UI không hiển thị lại.
6. **Select scope một danh mục** — API nhận nhiều `category_ids`, UI mới chỉ chọn một.
7. **`/uploads/presign` không áp scope** — thuộc Đợt 26 (xem `technical_debt/SCOPE_ENFORCEMENT_GAPS.md`).

## 9. Việc tiếp theo: biểu đồ, LLM tự chọn loại (Đợt 26 — chưa cài)

Yêu cầu: answer markdown đã ổn → thêm **biểu đồ**, và **chính LLM quyết định vẽ
loại nào**. Thiết kế đã chốt ở `RAG_RESEARCH.md` §6.9, tóm lại để demo khỏi nói
ngược với tài liệu nghiên cứu:

- **LLM chọn, server dựng số.** Tool `chart_from_cite` nhận `{n, kind:
  bar/line/pie, title}` — `n` là **số thứ tự trích dẫn của lượt hiện tại**, `kind`
  là loại biểu đồ LLM tự chọn. Backend nạp lại chunk theo `(article_id,
  chunk_index)` từ Qdrant, tự trích số, dựng spec ECharts. Lý do cứng: *spec do
  model sinh là lỗ hổng* — model có thể tự thêm điểm dữ liệu, mà "số bịa đặt
  vào chart" trông thuyết phục hơn số bịa trong văn bản.
- **Validate server-side từng điểm**: `series[].data` phải khớp số trích được từ
  chunk; không khớp thì **từ chối vẽ** và trả cảnh báo cho agent, không vẽ nửa
  chừng.
- **DTO**: `ChatResponse.charts[]` = `{n, kind, spec, caption}` — nằm cạnh
  `cites[]`, không nhét JSON vào markdown.
- **FE**: `ngx-echarts@21` + `echarts/core` (Bar/Pie/Line, Grid/Tooltip); mỗi
  chart phải kèm dòng **"Nguồn: [n] tr.12 · ĐIỀU 13"** bấm được sang source
  panel, nếu không thì chart là "số liệu từ trên trời".
- **Quy tắc chọn loại** ( viết vào `prompts/chat_system.md`, không tách thành
  tool mới): so sánh rời rạc giữa các đối tượng → **bar**; diễn biến theo thời
  gian (≥3 mốc có thứ tự) → **line**; cơ cấu tổng thể ≤6 nhóm → **pie**;
  >6 nhóm hoặc người đọc cần số chính xác → **bảng markdown**, không vẽ.
- **Case nghiệm thu**:

  | ID | Câu hỏi | Mong đợi |
  |---|---|---|
  | X-30 | "Vẽ biểu đồ lương thực nhận 5 nhân viên" | bar 5 cột, đúng 5.300.000 / 7.200.000 / 3.870.000 / 10.000.000 / 3.600.000 |
  | X-31 | "Vẽ cơ cấu thu — chi tháng 10" | bar 2 cột 58.250.000 (chi) vs 103.400.000 (thu), kèm caption |
  | X-32 | "Vẽ diễn biến chi theo ngày trong tháng 10" | line 8 điểm ngày 03→28/10 |
  | X-33 | "Vẽ biểu đồ phụ cấp theo tháng" | **từ chối**: dữ liệu chỉ có mức theo tháng đơn lẻ, không có chuỗi thời gian → nói rõ thiếu gì, trả bảng |
  | X-34 | "Vẽ biểu đồ phí cấp bản sao" (2 nguồn khác đơn vị) | không nhét `bản/lần` và `trang` chung một trục; chọn một nguồn và ghi rõ nguồn nào |
  | X-35 | Vẽ từ tài liệu scan/chưa extract được bảng | từ chối (điều kiện bật chart ở §6.9 e.1), không đoán số |

