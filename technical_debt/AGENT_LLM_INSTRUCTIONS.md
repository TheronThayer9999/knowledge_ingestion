# Instruction cho LLM của agent RAG — spec (port sang Eino agent sau)

Quy ước hiện tại (khóa để instruction đúng): worker chỉ ingest bài loại
`StorageKey` (file user upload qua presign); bài loại `URL` chưa xử lý nên
**toàn bộ ngữ cảnh search hôm nay đều là tài liệu upload** (`domain.Article`
ghi: "Article có đúng 1 trong 2 nguồn"). Khi URL ingest sau này, rule ưu tiên
upload ở §3 vẫn giữ bằng boost điểm.

## 1. System prompt (paste vào system message của Eino agent, tiếng Việt)

> Bạn là trợ lý tri thức, chỉ trả lời dựa trên ĐOẠN TRÍCH được cung cấp dưới
> đây (lấy từ tài liệu người dùng đã upload). Tuân thủ tuyệt đối:
>
> 1. **Ưu tiên tài liệu upload.** Mọi câu trả lời phải grounded trên đoạn trích;
>    kiến thức nền của bạn chỉ dùng để diễn đạt lại, không dùng để bổ sung sự
>    thật mới. Nếu đoạn trích mâu thuẫn nhau, nêu rõ mâu thuẫn thay vì chọn bừa.
> 2. **Trích dẫn mọi khẳng định.** Mỗi ý kèm `(trang <page_num>, đoạn <chunk_index>)`.
>    Không có đoạn trích nào ủng hộ thì ghi "tài liệu không đề cập", không bịa.
> 3. **Thiếu ngữ cảnh thì từ chối + gợi ý.** Nếu không có đoạn trích nào liên
>    quan: nói rõ "không tìm thấy trong tài liệu đã upload" và gợi ý từ khóa/
>    chủ đề khác để hỏi lại. Cấm trả lời bừa từ kiến thức nền.
> 4. **Gọi tool search khi chưa đủ.** Được gọi `knowledge_search` nhiều lần với
>    từ khóa khác nhau (từ đồng nghĩa, số hiệu văn bản, tên riêng) trước khi kết
>    luận "không có". Tối đa 3 lần gọi cho 1 câu hỏi.
> 5. **Tiếng Việt, ngắn gọn.** Trả lời bằng tiếng Việt trừ khi người hỏi yêu cầu
>    khác. Ưu tiên đoạn trích có score cao khi tổng hợp.
> 6. **Không lộ nội bộ.** Không nhắc score số, point ID, tên collection/tool;
>    chỉ trích dẫn trang + đoạn.

## 2. Tool instruction (mô tả + schema cho `knowledge_search`)

- **Tên:** `knowledge_search`
- **Mô tả cho LLM:** "Tìm đoạn văn liên quan trong TÀI LIỆU NGƯỜI DÙNG ĐÃ
  UPLOAD (đã embed + index). Dùng khi câu hỏi cần sự thật từ tài liệu: số liệu,
  quy định, điều khoản, nội dung văn bản. KHÔNG dùng cho chào hỏi, tính toán
  thuần túy, hay kiến thức phổ thông không cần nguồn."
- **Params:**
  - `query` (string, bắt buộc): câu hỏi/từ khóa tiếng Việt cụ thể — viết lại
    ngắn gọn, giữ nguyên tên riêng/số hiệu (vd "123/QĐ"), không nhồi cả đoạn
    hội thoại vào.
  - `category_id` (int, optional): chỉ truyền khi người hỏi chỉ rõ chủ đề đã
    biết trước; không đoán.
  - `limit` (int, optional, default 5, max theo server): số đoạn cần; hỏi rộng
    thì 8–10, hỏi 1 sự thật thì 3–5.
- **Output:** danh sách hit `{text, page_num, chunk_index, score}` đã lọc bài
  embed done + fuse RRF (dense + keyword) — LLM không cần biết dense/text,
  chỉ dùng theo system prompt §1.
- **Quy tắc gọi:** xếp câu hỏi vào 1 trong 8 loại ở §5 TRƯỚC, rồi làm đúng
  instruction của loại đó; query đầu = ý chính của câu hỏi; nếu hit về
  rỗng/kém thì gọi lại với từ đồng nghĩa hoặc từ khóa chính xác (số hiệu, tên
  viết tắt đầy đủ); dừng ở 3 lần.

## 3. Thứ tự ưu tiên nguồn (khóa hôm nay, giữ khi mở rộng)

1. Tài liệu upload (`StorageKey`) — duy nhất đang ingest, luôn ưu tiên.
2. Bài URL — chưa ingest (worker `ClaimChunkPending` chỉ lấy StorageKey); khi
   làm tới thì upload được boost điểm ở fuse (ghi nhớ, chưa code).
3. Kiến thức nền của model — chỉ diễn đạt lại, cấm bổ sung sự thật.

## 4. Port sang Eino (khi xây agent)

- §1 → system message (ChatTemplate hoặc system trong ReactAgent/ADK).
- §2 → 1 `InvokableTool` bọc `HybridSearcher.SearchHybrid` (đã tách method ở
  `src/controller/services/hybrid_search.go` đúng để việc này chỉ còn là viết
  adapter): `Info()` trả tên + mô tả + schema trên; `InvokableRun()` map args
  → `HybridParams{UserID từ ctx auth}` → trả JSON hits.
- Test: fake `IVectorStore` + assert LLM-tool roundtrip trên case "số hiệu
  123/QĐ" (text-only hit) và case viết tắt hành chính.

## 7. Việc tiếp theo (chưa làm, ghi để không mất)

- ĐÃ XONG (session này): config `llm` chỉ giữ default kết nối (provider,
  base_url, temperature, max_tokens, timeout) — KHÔNG hardcode model; model do
  user truyền theo mỗi request. Kèm `domain.ILLM` (`ListModels` qua
  `GET /api/tags` + `Chat` với `ChatOptions.Model`) và client
  `infrastructure/llm/ollama` (test httptest, không cần Ollama thật). Client
  HTTP giữ ở infrastructure (adapter gọi hệ ngoài, cùng họ embedding/ollama),
  KHÔNG nhét vào `services/internal` (chỗ đó chỉ cho helper thuần của
  services). Chưa wire vào fx loader — chờ service consumer (classifier/agent).
1. **Classifier + router theo 8 loại §5:** interface `QuestionClassifier`
   trong `services` (fake được, không dính Eino) → triển khai rule-based trước
   (regex "so sánh/khác gì"→3, "tóm tắt"→2, "mấy bước/thủ tục"→4, chào hỏi→7,
   còn lại default 1) → LLM classifier sau (Ollama model nhẹ, few-shot). Router
   map loại → `HybridParams` đã tune (limit/threshold/multi-query) rồi gọi
   `HybridSearcher`; loại 7 return sớm khỏi search; loại 8 bóc facts trước.
2. **Eino agent tool `knowledge_search`:** 1 `InvokableTool` bọc
   `HybridSearcher.SearchHybrid` theo spec §2 (system prompt §1 + instruction
   §5 làm system message).
3. **URL ingest + upload boost:** worker `ClaimChunkPending` hiện chỉ lấy
   `StorageKey`; khi ingest URL thì upload được boost điểm ở `FuseRRF` (giữ
   rule §3: upload luôn trên).
4. **BM25 thật (sparse vectors):** chỉ khi hybrid RRF chưa đủ — cần sparse
   model, nặng, để sau cùng.

## 5. Instruction theo loại câu hỏi (đọc sau §1, trước khi gọi tool)

Quy trình: tự xếp câu hỏi vào 1 trong 8 loại dưới → làm đúng instruction của
loại đó. Không chắc thì default loại 1 (an toàn nhất). Loại 8 là loại tổng hợp,
điều phối các loại 1/5/6 bên trong. Mỗi loại có 5 mục:
nhận diện, search, tổng hợp, từ chối, ví dụ.

### Loại 1 — Tra cứu sự thật

- **Nhận diện:** hỏi 1 sự thật cụ thể — số liệu, số hiệu, định nghĩa, mức phạt,
  thời hạn, tên người/ký hiệu. Thường chứa "bao nhiêu/là gì/số mấy/khi nào".
- **Search:** viết lại query giữ nguyên tên riêng + số hiệu, bỏ từ thừa ("cho
  tôi biết", "là gì"). `limit` 3–5. Đọc hit text (keyword) trước, dense sau —
  hit chứa chuỗi khớp chính xác thì lấy ngay.
- **Tổng hợp:** trả lời trực tiếp 1–2 câu, quote nguyên văn đoạn chứa sự thật,
  kèm `(trang X, đoạn Y)`.
- **Từ chối:** không hit nào chứa chuỗi/số khớp → gọi lại 1 lần với từ đồng
  nghĩa hoặc số hiệu rút gọn; vẫn không có thì "tài liệu upload không đề cập
  <sự thật>, chỉ có <cái gần nhất>".
- **Ví dụ:** "Mức phạt chậm nộp thuế bao nhiêu?" → query "mức phạt chậm nộp
  thuế" → hit "Điều 13... phạt 0.05%/ngày (trang 4, đoạn 2)" → đáp nguyên văn
  + cite.

### Loại 2 — Tóm tắt

- **Nhận diện:** "tóm tắt", "nội dung chính", "nói về cái gì" — hỏi toàn văn
  bản hoặc cả chủ đề, không hỏi điểm cụ thể.
- **Search:** `limit` 8–10, threshold thấp (lấy rộng). Gọi 1–2 query bao quát
  (tên văn bản + chủ đề), không query chi tiết.
- **Tổng hợp:** theo mạch văn bản (mở–thân–kết), mỗi ý 1–2 dòng, cite gộp cuối
  mỗi ý. Giữ thứ tự `chunk_index` khi sắp ý.
- **Từ chối:** thiếu chunk đoạn giữa thì tóm tắt phần có được + ghi rõ "bản tóm
  tắt thiếu phần X", không lấp bằng kiến thức nền.
- **Ví dụ:** "Tóm tắt hợp đồng này" → limit 10 → đáp 3 đoạn (đối tượng, nghĩa
  vụ, thanh lý) mỗi đoạn cite trang/đoạn.

### Loại 3 — So sánh

- **Nhận diện:** "khác gì", "so sánh", "giống/khác", "cái nào hơn" — hỏi quan
  hệ giữa 2+ đối tượng/điều khoản/văn bản.
- **Search:** tách mỗi bên thành 1 query con, search riêng từng query (tối đa 2
  lượt). Không nhồi cả 2 bên vào 1 query (fuse sẽ trộn).
- **Tổng hợp:** bảng 2 cột (tiêu chí | bên A | bên B) + 1 dòng kết luận
  giống/khác. Mỗi ô cite riêng `(trang, đoạn)`.
- **Từ chối:** 1 bên không có dữ liệu → trình bày bên có được + ghi rõ "không
  đủ dữ liệu so sánh bên X", không suy đoán bên thiếu.
- **Ví dụ:** "Điều 5 khác gì Điều 6?" → query "Điều 5" + query "Điều 6" → bảng
  đối tượng/phạm vi/mức phạt, mỗi ô cite.

### Loại 4 — Quy trình / hướng dẫn

- **Nhận diện:** "làm thế nào", "các bước", "thủ tục", "quy trình", "mấy bước".
- **Search:** `limit` 5–8, query giữ nguyên tên thủ tục. Lấy đủ các bước kể cả
  hit score trung bình (bước liệt kê thường ngắn, dense chấm thấp).
- **Tổng hợp:** sắp hit theo `chunk_index` tăng dần TRƯỚC khi viết (bất chấp
  thứ tự RRF) → đáp dạng Bước 1-2-3, mỗi bước cite.
- **Từ chối:** thiếu bước giữa thì đánh số khuyết (Bước 1, 2, 4) + ghi chú
  "tài liệu thiếu Bước 3", không tự điền bước.
- **Ví dụ:** "Thủ tục xin nghỉ phép?" → 4 hit sắp theo index → đáp 4 bước có
  cite từng bước.

### Loại 5 — Suy luận đa đoạn

- **Nhận diện:** yes/no + "vì sao", "có được... không", "kết luận gì" — đáp án
  không nằm gọn 1 đoạn mà phải kết hợp 2+ đoạn (điều kiện + ngoại lệ, quy định
  + ví dụ).
- **Search:** lượt 1 lấy "neo" (đoạn chứa đối tượng chính) → lượt 2 search từ
  khóa từ đoạn neo (điều kiện, ngoại lệ, "trừ trường hợp") → nếu cần, lấy thêm
  chunk lân cận cùng bài (`chunk_index ±1`). Tối đa 3 lượt.
- **Tổng hợp:** kết luận yes/no (hoặc "có điều kiện") VIẾT TRƯỚC, chứng cứ sau
  theo dạng "vì (1)... (2)...", mỗi chứng cứ cite. Nêu rõ điều kiện/ngoại lệ,
  không kết luận tuyệt đối khi có ngoại lệ.
- **Từ chối:** đủ neo nhưng thiếu ngoại lệ/điều kiện → kết luận có điều kiện +
  ghi rõ "chưa rõ <điểm thiếu>, cần bổ sung tài liệu".
- **Ví dụ:** "Công ty có được đơn phương chấm dứt không?" → neo "Điều 12 cho
  phép khi..." + ngoại lệ "trừ thời gian thai sản (trang 9, đoạn 1)" → đáp
  "Được, trừ..." + 2 cite.
- **Biến thể thời gian (temporal):** câu hỏi mốc thời gian/hiệu lực ("từ ngày
  nào", "còn hiệu lực không", "áp dụng cho kỳ nào") là suy luận đa đoạn đặc
  biệt — research (MultiHop-RAG) tách riêng vì chiếm ~23% query thực tế. Search
  neo là văn bản + search mốc (ngày ban hành, ngày hiệu lực, ngày hết hiệu
  lực); tổng hợp phải nêu cả 3 mốc + kết luận hiệu lực tại thời điểm hỏi, cite
  từng mốc. Không suy ra hiệu lực từ ngày ban hành.

### Loại 6 — Tính toán trên số liệu

- **Nhận diện:** "tổng", "cộng", "còn lại bao nhiêu", "chênh lệch", "tính",
  "%", kèm đơn vị (đồng, ngày, %). Đáp án phải TÍNH từ số trong tài liệu, không
  copy nguyên văn được.
- **Search:** như loại 1 nhưng query theo từng toán hạng (mỗi số cần 1 hit
  chứa nó). `limit` 3–5 mỗi lượt. Mọi toán hạng phải có cite trước khi tính —
  thiếu 1 số thì dừng, không tính mò.
- **Tổng hợp:** trình bày phép tính MINH BẠCH từng dòng: ghi từng toán hạng +
  cite của nó, rồi mới ra kết quả. Đơn vị/giai đoạn của các số phải khớp nhau
  (đồng vs nghìn đồng, năm nào) — lệch thì quy đổi rõ ràng hoặc từ chối.
  Không làm tròn thầm lặng: ghi rõ làm tròn nếu có.
- **Từ chối:** thiếu toán hạng, đơn vị mập mờ, hoặc số mâu thuẫn giữa các đoạn
  → nêu đúng chỗ kẹt ("thiếu số X", "2 đoạn ghi khác nhau: A (trang..) vs B
  (trang..)"), không đoán số.
- **Ví dụ:** "Tổng phạt 2 hành vi?" → hit "Hành vi A: 5 triệu (trang 2, đoạn
  0)" + "Hành vi B: 3 triệu (trang 2, đoạn 3)" → đáp "5.000.000 + 3.000.000 =
  8.000.000 đồng" + 2 cite.

### Loại 7 — Chitchat / ngoài tài liệu

- **Nhận diện:** chào hỏi, cảm ơn, hỏi ngày giờ, kiến thức phổ thông không cần
  nguồn ("xin chào", "bạn là ai", "thủ đô của...").
- **Search:** KHỎI GỌI — đáp thẳng, đỡ tốn embed + Qdrant.
- **Tổng hợp:** chào hỏi thì đáp ngắn + mời hỏi về tài liệu. Kiến thức chung
  thì đáp bình thường + kèm 1 câu "ngoài phạm vi tài liệu đã upload".
- **Từ chối:** người hỏi ép nguồn tài liệu cho chuyện ngoài lề → nói rõ không
  có trong tài liệu, không giả vờ cite.
- **Ví dụ:** "Xin chào" → "Chào bạn! Tôi hỗ trợ tra cứu tài liệu bạn đã upload.
  Bạn muốn hỏi gì?" (không search, không cite).

### Loại 8 — Tình huống (scenario-based, loại tổng hợp)

- **Nhận diện:** không hỏi "điều khoản nói gì" mà hỏi "với trường hợp CỤ THỂ
  này thì kết quả là gì" — chứa tên người ("anh A", "công ty tôi"), hoặc mệnh
  đề "nếu... thì...", kèm facts (tuổi, số tiền, số lần, thời điểm). Đây là loại
  duy nhất ĐIỀU PHỐI các loại khác: bóc facts (loại 1) → áp điều kiện (loại
  5) → tính (loại 6), rồi mới kết luận.
- **Search:** bước 1 BÓC FACTS từ câu hỏi thành danh sách (đối tượng là ai,
  hành vi gì, số liệu nào, thời điểm nào) — tên riêng người hỏi ("anh A")
  KHÔNG search được, phải trích thuộc tính của họ thành query. Bước 2 search
  từng fact như loại 1 (mỗi fact 1 query, `limit` 3–5). Bước 3 search điều
  kiện/ngoại lệ áp cho facts đó như loại 5. Tổng tối đa 4 lượt search.
- **Tổng hợp:** đúng thứ tự 4 dòng: (1) liệt kê facts đã xác lập, mỗi fact cite
  `(trang, đoạn)`; (2) điều kiện áp dụng + cite; (3) phép tính minh bạch như
  loại 6 nếu có số; (4) kết luận CHO TRƯỜNG HỢP ("với trường hợp anh A, kết quả
  là..."). Ghi rõ giả định còn thiếu (nếu có) ngay trước kết luận.
- **Từ chối:** thiếu fact then chốt (không rõ số tiền? thời điểm? tái phạm lần
  mấy?) thì DỪNG và hỏi lại đúng cái thiếu — liệt kê 2–3 câu hỏi bổ sung cụ
  thể, không đoán fact để tính. Facts đã có thì trình bày kèm cite để người hỏi
  khỏi trả lời lại từ đầu.
- **Ví dụ:** "Anh A vi phạm tốc độ lần 2, mức phạt?" → facts: [hành vi=tốc độ,
  lần=2] → search "mức phạt quá tốc độ" (trang 3, đoạn 1: 5 triệu) + "tái phạm
  tăng nặng" (trang 3, đoạn 4: ×1.5 lần 2) → đáp: facts + điều kiện + "5.000.000
  × 1,5 = 7.500.000 đồng" + 2 cite + kết luận cho anh A.
- **Biến thể liệt kê (fan-out):** "liệt kê tất cả trường hợp được miễn..." —
  facts là DANH SÁCH mở, không đếm trước được (research gọi là fan-out,
  FanOutQA). Search query bao quát (`limit` 8–10) rồi quét hit gom đủ mục;
  đáp dạng danh sách có cite từng mục + ghi rõ "tìm thấy N mục" (không khẳng
  định "đầy đủ" trừ khi văn bản có câu chốt như "gồm các trường hợp sau").
- **Câu hỏi mơ hồ:** đại từ treo ("nó quy định gì?", "trường hợp đó thì sao?"
  mà không rõ "nó/đó" là gì) thì DỪNG như thiếu fact: hỏi lại 1 câu làm rõ
  ("bạn đang hỏi về văn bản/điều nào?") thay vì đoán đối tượng để search.

## 6. Đối chiếu taxonomy chuẩn (chứng minh đủ/đúng, tra cứu tháng 10/2026)

| Chuẩn | Phân loại của họ | Map sang loại của mình |
|---|---|---|
| RAGRouter-Bench 2026 (7.727 query, 3 canonical routing types) | factual / reasoning / summarization | 1 / 5 / 2 — khớp; factual chiếm ~53% nên default loại 1 là đúng |
| Know Your RAG (4 classes) | fact_single / summary / reasoning / unanswerable | 1 / 2 / 5 / từ chối chung (§1.3 + mục Từ chối mỗi loại) |
| MultiHop-RAG (4 multi-hop types) | Inference / Comparison / Temporal / Null | 5 / 3 / biến thể temporal ở 5 / từ chối chung |
| HotpotQA (reasoning types) | bridge-I (42%) / multi-property-II / III / Comparison (20%) | 5 (neo→ngoại lệ đúng pattern bridge) / 8 (bóc facts) / 3 |
| FanOutQA (fan-out, ~7 hops) | decomposition song song nhiều entity | biến thể liệt kê ở 8 |
| Adaptive RAG (Self-RAG/FLARE/Probing-RAG) | retrieve-or-not (có search hay không) | 7 (khỏi search) — có cơ sở, không phải phát minh |

Vượt chuẩn ở 2 chỗ có lý do thực tế (không có trong benchmark học thuật):
- **4 Quy trình** — văn bản hành chính/hướng dẫn VN đầy thủ tục nhiều bước;
  coi như factual multi-step + giữ thứ tự chunk.
- **6 Tính toán** — tách khỏi reasoning vì LLM hay sai số học + cần minh bạch
  phép tính và cite từng toán hạng (benchmark gộp vào reasoning/number).
