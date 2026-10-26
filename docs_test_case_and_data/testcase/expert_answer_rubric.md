# Rubric chấm "trả lời như chuyên gia" (Đợt 25)

Mục 10 của `prompts/chat_system.md` đã quy định *cách trình bày* câu trả lời.
"Văn hay" không assertion được, nên tài liệu này chuyển các gạch đầu dòng đó
thành tiêu chí chấm điểm, phần nào máy chấm được thì giao cho
`loadtest/expert_eval.py`, phần còn lại để người chấm.

Rubric chỉ đo **hình thức**. Mọi quy tắc về căn cứ (mục 1) đứng trước: một câu
văn mượt nhưng bịa số là điểm 0 về căn cứ, không phải 5 về hình thức.

## 1. Cách chấm

- Mỗi **lượt hỏi × mỗi provider** = một dòng điểm (9router / nvidia /
  openrouter chấm riêng để so sánh được, không gộp).
- Thang 1–5:

  | Mức | Ý nghĩa |
  |---|---|
  | 1 | Sai dữ kiện, hoặc đùn đẩy ("bạn tự kiểm tra lại tài liệu") |
  | 2 | Đúng dữ kiện nhưng là máy trả lời: liệt kê kết quả tìm được, mở đầu bằng việc sắp làm |
  | 3 | Đúng và đủ, còn dài dòng hoặc trượt 1 tiêu chí hình thức |
  | 4 | Đúng, gọn, đủ bối cảnh; còn 1 chỗ câu chữ chưa tự nhiên |
  | 5 | Như người có nghề trả lời: vào thẳng, đúng thuật ngữ của tài liệu, chốt bằng việc cần làm |

- **Đạt** khi: trung bình ≥ 4 **và** không tiêu chí nhóm A nào < 4 **và** không có
  lỗi loại trừ (mục 4).
- Câu nhiều ý (dạng cặp A→B) chấm **từng lượt**, không chấm trung bình cả cặp,
  vì lỗi hội thoại lộ ra ở lượt thứ hai.

## 2. Tiêu chí

### Nhóm A — căn cứ & nội dung (người chấm)

| # | Tiêu chí | Đạt | Trượt |
|---|---|---|---|
| A1 | Đúng dữ kiện | Mỗi số liệu khớp đúng ô sheet / dòng trang; nói rõ đơn vị | Sai bất kỳ số nào → tối đa 2 cho cả bảng |
| A2 | Đủ bối cảnh trước phán quyết | Nêu điều kiện/giới hạn/ngoại lệ cùng Điều cùng sheet rồi mới chốt | Chốt từ một khoản chung, bỏ qua khoản hạn chế |
| A3 | Không vượt quá nguồn | Phần không có trong tài liệu được gắn nhãn rõ | Trộn "thông lệ thường thấy" vào quy định của tổ chức |
| A4 | Xử lý nhiều nguồn / nhiều phiên bản | Bản có hiệu lực mới thắng; nêu rõ bản bị bãi bỏ và ngày | Trích bản cũ như hiện hành, hoặc cộng gộp hai bản |
| A5 | Đúng phạm vi đọc | Liệt kê "đã đọc sheet nào, trang nào, phần nào còn thiếu" | Khẳng định "tất cả" khi chưa đọc đủ |

### Nhóm B — hình thức (machine = đã có heuristic trong `expert_eval.py`)

| # | Tiêu chí | Máy | Đạt | Trượt |
|---|---|---|---|---|
| B1 | Kết luận trước, chi tiết sau | `verdict_first` | 1–2 câu đầu trả lời thẳng (được/không/bao nhiêu) kèm `[^n]` | Mở đầu bằng dẫn nhập, ý chính nằm giữa bài |
| B2 | Không mô tả việc sắp làm | `no_meta_opener` | Không "tôi sẽ tra cứu", "dựa trên kết quả tìm kiếm" | Có cụm mở đầu kiểu meta |
| B3 | Không sáo ngữ | `no_filler` | Không khen câu hỏi, không cảm ơn/xin lỗi, không chúc chung chung | Có sáo ngữ |
| B4 | Không lộ nội bộ hệ thống | `no_plumbing_leak` | Không tên tool, điểm số, ID, collection | "công cụ search_articles trả về 5 đoạn, score 0.82" |
| B5 | Không tóm lại điều vừa nói | `no_self_summary` | Chốt bằng việc cần làm | "Tóm lại, tôi đã trình bày ở trên..." |
| B6 | Mật độ trích dẫn | `citation_density` | ≥ 1 `[^n]` cho mỗi ~800 ký tự | Đoạn dài không có căn cứ |
| B7 | Độ dài tương xứng | người | Câu một dữ kiện trả lời trong 1–3 câu | Chia tiêu đề + danh sách cho một con số |
| B8 | Giải thích thuật ngữ đủ hiểu | người | Giải 1 câu cho thuật ngữ dùng lần đầu, không giải thứ người hỏi đã biết | Dạy lại khái niệm hiển nhiên, hoặc dùng từ chuyên môn mà không định nghĩa |
| B9 | Để lộ lý lẽ đúng chỗ | người | Câu tính toán/suy luận trình từng mắt xích; câu tra cứu thì không bày bước | Suy luận mà chỉ cho kết quả, hoặc tra cứu mà viết thành bài luận |
| B10 | Quả quyết đúng mức | người | Căn cứ đủ thì khẳng định; mới đọc một khoản thì nói rõ phần chắc/phần chưa | "có lẽ", "thường thì" với nội dung tài liệu; hoặc chốt chắc khi chưa đọc đủ |

## 3. Ví dụ đạt / trượt (rút từ fixture thật)

Câu hỏi: *"Vì sao NV05 chỉ nhận phụ cấp ăn trưa 600.000đ trong khi NV01 nhận
800.000đ?"*

**Trượt (B1, B2, B4)** — máy liệt kê kết quả tìm được:

> Dưới đây là thông tin tôi tìm được từ tài liệu. Sheet Phu_cap có các dòng:
> NV01 phụ cấp ăn trưa 800.000, NV05 600.000. Sheet Luong có NV01 22.5 ngày
> công, NV05 20 ngày công. Điểm similarity của các đoạn là 0.86 và 0.79.

**Đạt (5)** — vào thẳng, đúng thuật ngữ, để lộ mắt xích, chốt ràng buộc:

> Vì hai người không cùng điều kiện hưởng, không phải do tính nhầm. Phụ cấp ăn
> trưa gắn với ngày công: từ 22 ngày công trở lên thì 800.000đ/tháng, dưới 22
> ngày thì 600.000đ/tháng [^3]. NV01 ghi 22,5 ngày công nên thuộc nhóm trên
> [^1]; NV05 chỉ 20 ngày và sheet phụ cấp ghi rõ "NV05 nghỉ 5 ngày trong quý"
> nên thuộc nhóm dưới 22 ngày [^3]. Lưu ý kỳ này là Quý 3/2025 (01/07–30/09),
> mức phụ cấp áp dụng theo tháng [^2].

Câu hỏi nhiều phiên bản: *"Cấp bản sao 30 trang hết bao nhiêu, theo bản nào?"*
→ đáp án đúng là 70.000đ theo **Phụ lục A của quy định v3.0** (10 trang đầu
3.000/trang, 20 trang sau 2.000/trang) [^phụ lục A], **không phải** biểu phí
xlsx v1 (5.000đ/bản) hay v2 (8.000đ/bản) — hai biểu phí này là dịch vụ khác
đơn vị tính (bản/lần vs trang). Câu nào cộng gộp hai nguồn khác đơn vị là trượt A4.

## 4. Lỗi loại trừ (dừng, không chấm tiếp dòng đó)

- `answer` rỗng hoặc 503 `agent produced no final answer`: quay lại đúng 1 lần;
  vẫn lỗi thì **gạch dòng** và ghi nguyên nhân hạ tầng — không chấm 0, vì sẽ
  lẫn lỗi transport với lỗi chất lượng.
- Provider trả về khác provider được yêu cầu (`meta.provider` drift): lỗi cấu
  hình rotation; gạch cả dòng.
- `[^n]` vượt quá số lượng `cites` (marker ma): mọi tiêu chí nhóm A tối đa 2,
  vì không đối chiếu được câu nào với tài liệu nào.
- Số tài chính ≥ 1.000 không trích được từ `cites`/đáp án: treo cờ
  `FINANCIAL-NOT-VERIFIED` và **người xác nhận trước** khi kết luận là bịa —
  tiền cộng đúng vẫn xuất hiện hợp lệ (48.750.000 + 81.600.000 = 130.350.000).

## 5. Bảng chấm mẫu

```
| Case | Provider | A1 | A2 | A3 | A4 | A5 | B1..B6 (máy) | B7 | B8 | B9 | B10 | TB | Ghi chú |
|------|----------|----|----|----|----|----|----------------|----|----|----|-----|----|---------|
| X-03 | 9router  |  5 |  4 |  5 |  3 |  - | 6/6            |  5 |  4 |  5 |   4 | 4,3 | A4: không nêu ngày bãi bỏ v1 |
```

Đổ kết quả thô vào `loadtest/expert_eval_results.json` (`--out`), trong đó mỗi
sub-task có `l0` / `l1` / `l2` — cột `l2.failed` cho biết đúng tên tiêu chí
trượt để người chấm khỏi đọc lại cả câu.
