# Test Context liên tiếp (Multi-turn Memory)

Mục đích: kiểm tra agent có giữ được context giữa 2 câu hỏi liên tiếp hay không,
và đo lường mức độ dùng lại thông tin của câu trước.
## Phương pháp

Chạy từng cặp câu hỏi liên tiếp theo 2 chế độ:

| Chế độ | Cách chạy | Kỳ vọng |
|--------|-----------|---------|
| `fresh` (mặc định) | Q1 rồi Q2, không truyền history | Q2 KHÔNG thấy Q1 → không trả lời được tham chiếu |
| `memory` | Q1 rồi Q2, truyền `chat_history` các lượt trước | Q2 dùng được fact của Q1 |

Mỗi câu Q2 chứa tham chiếu (đại từ) về câu Q1 ("con số vừa nói", "mức đó", "vậy còn")
nên chỉ trả lời đúng khi có context. LLM-as-Judge chấm 1-5 mức độ Q2 dùng context.

## Các cặp câu hỏi

### Pair A — Tham chiếu con số vừa trả lời
- **Q1:** Mức hỗ trợ ăn trưa mỗi ngày cho nhân viên làm việc tại văn phòng là bao nhiêu?
- **Đáp án Q1:** 30.000 VNĐ/ngày (Điều 6, Khoản 1).
- **Q2:** Con số vừa trả lời áp dụng cho đối tượng nào và theo Điều mấy?
- **Đáp án Q2 (khi có context):** 30.000 VNĐ áp dụng cho nhân viên làm việc tại văn phòng, Điều 6 Khoản 1.
- **Dấu hiệu mất context:** "Tôi không có thông tin về 'con số vừa trả lời'", hoặc trả lời chung chung không gắn với 30.000 VNĐ.

### Pair B — So sánh 2 con số ở 2 lượt
- **Q1:** Mức hỗ trợ gửi xe tại văn phòng mỗi tháng là bao nhiêu?
- **Đáp án Q1:** 500.000 VNĐ/tháng (Điều 6, Khoản 2).
- **Q2:** Con số vừa nói gấp bao nhiêu lần con số hỗ trợ ăn trưa mỗi ngày?
- **Đáp án Q2 (khi có context):** 500.000 / 30.000 ≈ 16.67 lần.
- **Dấu hiệu mất context:** không xác định được "con số vừa nói" là con số nào, hoặc tự bịa.

### Pair C — Tiếp nối quy định ở câu trước
- **Q1:** Điều 1 quy định nhân viên chính thức được làm việc từ xa tối đa bao nhiêu ngày mỗi tuần?
- **Đáp án Q1:** Tối đa 02 ngày/tuần (Điều 1, Khoản 1).
- **Q2:** Vậy trong tuần có dịch bệnh thì con số đó thay đổi thế nào theo tài liệu?
- **Đáp án Q2 (khi có context):** Trong tuần có dịch bệnh được remote tối đa 14 ngày mà không cần đăng ký (Điều 10, Khoản 3).
- **Dấu hiệu mất context:** không biết "con số đó" là con số nào, trả lời lạc đề.

### Pair D — Tổng tiền từ con số câu trước
- **Q1:** Mức hỗ trợ tiền ăn mỗi ngày khi công tác xa là bao nhiêu?
- **Đáp án Q1:** 200.000 VNĐ/ngày (Điều 14, Khoản 1).
- **Q2:** Vậy tổng tiền ăn cho 3 ngày công tác đó là bao nhiêu?
- **Đáp án Q2 (khi có context):** 200.000 × 3 = 600.000 VNĐ.
- **Dấu hiệu mất context:** không biết "mức đó" là bao nhiêu để nhân.

## Rubric chấm điểm (LLM-as-Judge, 1-5)

- **5:** Nhận diện đúng tham chiếu và dùng ĐÚNG fact từ câu Q1 để trả lời.
- **4:** Nhận diện tham chiếu, nội dung gần đúng, sai sót nhỏ.
- **3:** Trả lời một phần, còn mơ hồ về tham chiếu.
- **2:** Gần như bỏ qua context, trả lời như câu độc lập.
- **1:** Không nhận diện tham chiếu, tuyên bố "không có lịch sử hội thoại", hoặc bịa.

## Cách chạy

```bash
uv run python test_context.py --mode fresh    # hành vi hiện tại (kỳ vọng trượt context)
uv run python test_context.py --mode memory   # truyền chung ChatMemoryBuffer
```

Kết quả: bảng trên console + `context_test_report.json`.

## Tiêu chí đạt

- Mode `fresh`: điểm context trung bình ≤ 2 (xác nhận hành vi hiện tại không giữ context).
- Mode `memory`: điểm context trung bình ≥ 4 (xác nhận memory hoạt động khi truyền `memory=`).
