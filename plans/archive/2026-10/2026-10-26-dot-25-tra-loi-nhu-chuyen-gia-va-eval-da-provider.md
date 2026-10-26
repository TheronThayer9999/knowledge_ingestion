# Đợt 25 — Trả lời như chuyên gia + eval đa provider theo lô (9router / nvidia / openrouter)

Ngày: 2026-10-26 · Trạng thái: hoàn thành 2026-10-27 · Nối: Đợt 24 (`archive/2026-10/` — session chat lưu DB, S1+S4)

## 1. Yêu cầu đã nhận

Người dùng yêu cầu 4 việc, chốt lại thành một đợt:

1. **Nghiên cứu** cách các repo OSS nổi tiếng làm đúng tính năng này (lưu phiên chat, lịch sử, trích dẫn theo message) rồi đối chiếu với thiết kế mình đang có.
2. **Test bằng các LLM trong config**, xoay quanh **9router**, **nvidia**, **openrouter** (`configs/config.local.json` đã đủ key cho cả ba).
3. Hướng tới **AI trả lời như một chuyên gia** — từ câu văn tới cách giải thích.
4. **Test API theo lô**, vì giới hạn phía provider là **40 request/phút**.

Ràng buộc thêm: mọi commit message bằng **tiếng Anh**.

**Bổ sung sau khi chốt phạm vi (hỏi–đáp trực tiếp):** đợt này gồm **cả logic UI/UX của tính năng phiên chat ở FE** (Đợt 25b gộp vào đây), **nhiều test case tự nghĩ**, **data phức tạp sinh bằng script Python stdlib** (chat với Excel là mũi chính), và **tài liệu demo ghi tại `docs_test_case_and_data/testcase/`**. Người dùng tự test UI — tôi không mở browser, chỉ để lại checklist.

## 2. Nghiên cứu OSS — phát hiện và đối chiếu

Nguồn đã đọc trực tiếp (không suy đoán từ trí nhớ):

| Repo | Bằng chứng đã đọc | Cách họ làm | Mình đang ở đâu |
|---|---|---|---|
| **Dify** (`docs.dify.ai` — Service API `POST /chat-messages`, original OpenAPI `openapi_service.json`) | `ChatRequest` có `conversation_id` (rỗng = mở phiên mới, server sinh UUID và trả về); response `mode: chat` kèm `message_id` **và** `conversation_id`; citations nằm trong `metadata.retriever_resources[]`: `position`, `dataset_id/name`, `document_id/name`, `segment_id`, `content` (đoạn trích), `score`, `doc_metadata` | Server là nguồn sự thật duy nhất của lịch sử; client chỉ gửi `conversation_id`, **không gửi history**; citation **gắn với message**, có `usage` (prompt/completion tokens, latency, giá) | **Khớp gần tuyệt đối** với S1+S4: `conversation_id` optional, lịch sử nạp từ DB, `cites` jsonb per message. Khác: họ có `usage` per message — mình chưa có (xem §5.4) |
| **Open WebUI** (`backend/open_webui/models/chat_messages.py`, 1103 dòng) | Bảng `chat_message`: `chat_id` FK `ondelete=CASCADE`, `role`, **`parent_id`** (branch), `content` JSON (str hoặc list of blocks), `model_id`, `sources`/`files`/`embeds`/`meta` JSON, `done` (bool), `status_history`, `error`, `usage` JSON, **`context_summary` = "Context compaction checkpoint"**, 4 index trong đó `(chat_id, parent_id)` và `(chat_id, role, done)` | Trước đây họ nhét cả hội thoại vào một JSON blob của `chat`, **nay tách thành bảng message per-row**; `parent_id` phục vụ regenerate/fork; `context_summary` lưu **ngay trên hàng message** làm checkpoint nén ngữ cảnh | S1 đã đi đúng hướng họ vừa migrate (message per-row). S5 (regenerate/fork) = đúng `parent_id` của họ. S2 (nén luỹ tiến) = đúng `context_summary` trên hàng message, **không cần bảng riêng** — ghi lại để khi làm S2 không invent thêm schema |
| **RAGFlow** (blog 0.24.0 + HTTP API) | `extra_body.reference: true` (bật reference trong final chunk/final message), `reference_metadata.include` + `fields` (allowlist metadata kèm mỗi reference chunk), `metadata_condition` để lọc; 0.24 thêm **Memory API** riêng (CRUD memory + entries, tổ chức theo session, có log trích xuất) và "Agent chat management" giữ lịch sử hội thoại | hai tầng tách bạch: **chat history** (theo session) và **memory** (trích xuất dài hạn, có trạng thái trích xuất quan sát được); payload citation là **opt-in + có allowlist trường** | Mình chỉ có một tầng (lịch sử). Bài học áp dụng được: cho phép bật/tắt `snippet` trong cites để tránh phình jsonb (§5.5), còn Memory API thì **ngoài phạm vi đợt này** |
| **LangGraph** (docs persistence + `checkpoint-postgres`) | Checkpointer persist **state của một thread** dưới dạng snapshot per super-step, kèm **pending writes** cho node hỏng giữa chừng; tài liệu phân rõ short-term (thread-scoped checkpoints) vs long-term (Store cross-thread) | "một bước một snapshot" và "ghi phần đã xong khi bước hỏng" | Matches xung đột #16 trong `RAG_RESEARCH.md` (persist trước khi phát event `done`). Persist khi provider lỗi vẫn **không lưu gì** — đã test và ghi ở Đợt 24, giữ nguyên (quyết định có chủ đích, không phải thiếu sót) |
| **LlamaIndex** (docs `storing/chat_stores`) | `ChatStore` = `session_id -> danh sách message dict`; `MemoryBuffer` bọc chat store + summarizer, cắt theo **token limit** chứ không theo số lượt | window bằng ngân sách token | `ListRecent(limit=12)` đang cắt theo **số lượt**. Có chủ đích giữ nguyên đợt này (giới hạn đã ghi ở Đợt 24); S2/S3 mới đổi sang ngân sách token |
| **Ragas / DeepEval** (docs metrics) | Faithfulness = **tách answer thành từng claim rồi đối chiếu từng claim với context đã retrieve** (điểm = số claim được hỗ trợ / tổng số claim); FactualCorrectness so với reference; G-Eval = LLM-as-judge theo tiêu chí tự định nghĩa + chain-of-thought | chấm chất lượng = claim-level, không phải "có giống đáp án không" | `loadtest/e2e_testcases.py:grade()` mới so **token số** trong đáp án mong đợi. Đủ cho "đúng số", **không đo được** "chuyên gia". §6 dựng lớp rubric mới chồng lên |

**Kết luận nghiên cứu:** thiết kế S1+S4 đã chọn trùng cách làm của Dify/Open WebUI (server là nguồn lịch sử, citation gắn message, message per-row). Bốn khoảng thật sự còn hở, xếp theo giá trị:

1. **Chất lượng câu trả lời** không được đo bằng thứ gì cả (OSS/eval lib đều có rubric claim-level) → đây là việc chính của đợt này theo đúng ý "trả lời như chuyên gia".
2. **Không có harness đa provider theo lô** — provider xoay đã có trong `e2e_testcases.py` nhưng chỉ để *đồ thị hoá lỗi backend*, không phải để so chất lượng, và **không có bộ nhịp** (mỗi `/chat` bên trong là ~5-10 lượt gọi LLM — bằng chứng: log Đợt 24 một câu hỏi sinh 8 dòng `llm: request`, tổng 4m21s).
3. `usage` per message (Dify/Open WebUI đều có) — **defer**, thuộc nhóm S3 (ngân sách tool-result) vì cần cả số token từ Eino.
4. Allowlist trường trong `cites` (RAGFlow) — **defer**, chưa có vấn đề thực tế.

## 3. "Trả lời như chuyên gia" nghĩa là gì, thao tác được

Prompt `prompts/chat_system.md` (254 dòng) **đã rất mạnh về căn cứ** (4 bước plan→execute→verify→synthesize, bảng "Thông tin tối thiểu", 4 loại căn cứ A/B/C/D, chống prompt-injection, 12 ví dụ). Thứ nó **chưa** quy định là **hình dáng và giọng của câu trả lời** — mục 10 "Hình thức" hiện chỉ có 5 gạch: ngôn ngữ, ngắn gọn, theo định dạng user, in đậm số liệu, không lộ ID kỹ thuật. Chuyên gia khác máy tra ở chỗ: mở đầu bằng kết luận, giải thích *tại sao* chứ không chỉ *là gì*, chỉ ra điều kiện/ngoại lệ chưa kiểm tra, và nói bằng ngôn ngữ của nghề.

Thay đổi **thêm**, không viết lại (giữ toàn bộ quy tắc căn cứ — đó là phần đã được kiểm chứng):

- **Kết luận trước, căn cứ sau (BLUF):** 1-2 câu đầu trả lời thẳng câu hỏi, mỗi dữ kiện vẫn kèm `[^n]` ngay sau.
- **Giải thích như người trong nghề:** thuật ngữ dùng lần đầu được giải thích ≤ 1 câu bằng tiếng Việt thông dụng; khi người hỏi có khả năng hiểu sai (thuật ngữ pháp lý/ kế toán trùng với nghĩa đời thường) thì sửa hiểu lầm đó ngay, không chờ hỏi.
- **Đặt vấn đề vào bối cảnh áp dụng:** nêu điều kiện áp dụng/ngoại lệ/giới hạn đi cùng **trước khi** kết luận "được" hay "không được"; nếu chưa đọc đủ khoản thì nói rõ chưa đọc đủ (nối mục 11, không nới trần tool).
- **Tư duy thành tiếng có kiểm soát:** với câu suy luận/tính toán, hiển thị chuỗi mắt xích (mỗi mắt xích 1 dòng) thay vì chỉ kết quả; với câu tra cứu đơn giản thì **không** bày bước — tránh biến mọi trả lời thành bài luận.
- **Ngôn ngữ quả quyết đúng mức:** cấm "có lẽ", "thường thì", "ai cũng biết" cho nội dung tài liệu (đã cấm ở mục 1); cấm cả ngược lại — khẳng định chắc khi căn cứ chỉ trả lời một phần.
- **Kết bằng hành động:** khi câu hỏi mang tính tình huống, chốt bằng "bước tiếp theo nên làm là gì / cần bổ sung dữ kiện gì" — tối đa 2 dòng, không ra lệnh.
- **Không sáo ngữ:** bỏ mở đầu kiểu "Câu hỏi của bạn rất hay", không lặp lại nguyên văn câu hỏi, không cảm ơn, không xin lỗi, không tóm tắt lại chính mình ở cuối.
- **Độ dài tỷ lệ với độ phức tạp:** câu tra cứu 1 dữ kiện ≤ 3 câu; câu so sánh/tình huống nhiều điều kiện mới dùng tiêu đề + bảng.

Các mục này phải **tương thích** với 12 ví dụ đã có — nếu ví dụ nào mâu thuẫn thì sửa ví dụ, không để hai luật cùng chạy.

## 4. Rubric chấm (để "chuyên gia" không thành cảm tính)

Ba lớp, từ rẻ đến đắt, dùng chung một file kết quả:

- **Lớp 0 — hợp lệ kỹ thuật (tự động, không cần LLM):** HTTP 200; `answer` không rỗng; mọi marker `[^n]` trong answer có `n` nằm trong `cites` (bất biến Đợt 22); `cites` không rỗng khi câu hỏi thuộc nhóm "nội dung tài liệu"; `question_type` hợp lệ; provider trả lời = provider yêu cầu (phát hiện xoay provider ngoài ý muốn).
- **Lớp 1 — đúng dữ kiện (tự động, tái dùng `grade()`):** giữ nguyên logic so token số trong `Đáp án mong đợi`; cộng thêm: **số trong answer phải xuất hiện trong `snippet` của ít nhất một cite** (đây là bản đơn giản hoá của Ragas faithfulness — claim-level, làm được không cần LLM chấm).
- **Lớp 2 — hình thức chuyên gia (tự động bằng heuristics + người đọc xác nhận):** câu đầu tiên chứa kết luận (không phải "Tôi sẽ tìm..."); có/nhiều `[^n]` trên mỗi 500 ký tự; **không** chứa cụm sáo ngữ khoá (`rất hay`, `cảm ơn bạn`, `hãy cùng`, `là một AI`, `tôi hy vọng`); có cụm giải thích thuật ngữ khi câu hỏi chứa thuật ngữ; có "Phạm vi đã đọc"/nêu giới hạn khi là câu liệt kê; điểm = số đạt/số áp dụng.

Lớp 2 **không** dùng LLM-as-judge trong đợt này: thêm một LLM chấm một LLM sẽ kéo theo chi phí (gấp đôi lượt gọi trong trần 40/phút) và làm bằng chứng khó cãi hơn. Ghi lại thành hướng tiếp nối.

## 5. Kế hoạch thay đổi theo file

### 5.1 `prompts/chat_system.md`
Viết lại mục `# 10. Hình thức` thành `# 10. Hình thức: trả lời như một chuyên gia` theo §3 (thêm ~8-10 gạch, giữ 5 gạch cũ). Không đánh số lại mục nào → không ảnh hưởng các tham chiếu "mục 0/1/8/9/11" trong file.

### 5.2 `loadtest/expert_eval.py` (mới)
Harness chạy lô, **import lại** helper của `e2e_testcases.py` (`login`, `auth`, `register`, `create_category`, `presign_and_upload`, `wait_index_done`, `parse_cases`, `num_requirements`, `norm_vi`) — không fork logic upload/index-wait đã có.

CLI:
```
python expert_eval.py --providers 9router nvidia openrouter \
                      --batch-size 4 --rpm 40 --max 12 \
                      --file ..\docs_test_case_and_data\testcase\test_case.md
```
- **Bộ nhịp theo lô:** đếm cửa sổ 60 giây, mỗi cửa sổ phát tối đa `batch-size` câu `/chat`, nghỉ hết cửa sổ rồi đi lô sau. Mặc định `batch-size 4` vì **một câu `/chat` bên trong là nhiều lượt gọi LLM** (agent tool-loop + classify + retry; thực đo ở Đợt 24: 8-10 lượt/câu), 4 câu/cửa sổ ≈ 32-40 lượt LLM → nằm trong trần 40 req/phút. Ghi thẳng lý do con số 4 vào docstring để sau này không ai "tối ưu" thành 20.
- `--rpm` **chỉ là sàn tự vệ**: nếu provider thật thấp hơn (groq 30) thì hạ `batch-size`, không tự tăng request.
- **Xoay provider theo câu** (`--providers` round-robin như `e2e_testcases.py:230`), **tắt fallback xoay provider** (`--fallbacks` rỗng) để lỗi provider hiện đúng là lỗi provider, không bị provider khác cứu và làm bẩn so sánh.
- **Chạy có `conversation_id`:** mỗi provider mở một phiên riêng qua `POST /conversations`, mọi câu của provider đó đi vào phiên → vừa test đường S1/S4 thật, vừa **bịt kín khoảng hở Đợt 24** (chưa từng chứng minh `cites` khác rỗng bằng chứng live). Điều kiện tiền đề: tài liệu phải được **upload + index cho chính user chạy eval** (vector scope theo `user_id` — bài học đã có trong memory), nên harness bắt buộc `--docs` + chờ `index_status=done`, không cho `--skip-upload` khi chưa có vector cho user đó.
- Kết quả: `loadtest/expert_eval_results.json` (mỗi câu: provider đã trả lời, verdict 3 lớp, cites count, secs, conversation_id, message_id, answer) + bảng tổng hợp theo provider in ra console. Exit code ≠ 0 nếu có ERROR hoặc fail Lớp 0.
- **Không đụng API key:** mọi lượt gọi LLM đi qua `/chat` của chính server — harness **không đọc `configs/config.local.json`**, nên không có đường nào để key rò vào log/report. Report chỉ ghi tên provider/model.
- **User eval mới tinh** (`register` username `ev<hex>`): toàn bộ bài upload, category, phiên chat nằm trong một user dễ dọn, không lẫn với dữ liệu user khác (bài học Đợt 24: vector của user 8 `index_status=done` nhưng Qdrant không còn point nào).
- **Hai kiểu case** (đã đọc file thật): `test_case.md` — block `### Case N` 1 lượt; `context_test.md` — block `### Pair X` có `Q1/Q2` + `Đáp án Q2 (khi có context)` và `Dấu hiệu mất context`. Pair chạy **hai lượt trong cùng một phiên, lượt sau không gửi history từ client** → đây chính là phép thử "server là nguồn lịch sử" bằng LLM thật, không phải fake.
- **`--concurrency` mặc định 1**, nhận tối đa 4 (kẹp cứng + in cảnh báo): mỗi `/chat` tự nó đã là 8-10 lượt LLM trong 60-300 giây, nên chạy tuần tự đã dưới trần 40 req/phút rất xa; tăng concurrency là để rút ngắn thời gian, không phải để bấm nhanh hơn trần.

### 5.3 `docs_test_case_and_data/testcase/expert_answer_rubric.md` (mới)
Rubric §5 dưới dạng checklist để người đọc chấm nhanh 3-5 câu tiêu biểu per provider, và để sau này biến thành judge tự động.

### 5.4 `configs/config.local.json` (KHÔNG commit — gitignored)
Đặt `rate_limit_rpm: 35` cho `9router`, `nvidia`, `openrouter` để token-bucket phía server (`src/agent/eino/model.go:newRateLimitTransport`) thành lớp phòng thủ thứ hai; `retryTransport` đã tự xử lý 429. Ghi lại trong `## Kết quả` giá trị đã đặt (không ghi key).

### 5.5 `RAG_RESEARCH.md`
Thêm mục **§6.7.1 Đối chiếu OSS (Đợt 25)** với bảng phát hiện §2 + đường dẫn nguồn, để lần sau làm S2/S5 không phải đọc lại 4 repo.

**Không làm đợt này** (ghi rõ để khỏi lan man): `usage` per message; allowlist trường cites; `context_summary` cho S2; Memory-style store; LLM-as-judge.

## 5A. UI/UX logic cho phiên chat ở FE (Đợt 25b gộp vào)

Hiện trạng đã đọc code: `pages/chat/chat.ts` (290 dòng) **không biết gì về phiên** — `turns` chỉ là signal trên RAM, `reset()` xoá RAM, `HISTORY_LIMIT = 12` gửi history từ client (`core/chat/chat.service.ts:22-35`), sidebar hiện là "panel tiện ích" (phạm vi + phiên mới). BE đã đủ 6 route `/conversations*`.

**Quyết định contract (mấu chốt, chốt ở đây để không phải đổi DTO/swagger):**

- FE **tạo phiên bằng `POST /conversations` ngay trước lượt gửi đầu tiên** (lazy — bấm "Phiên mới" chưa tạo row nào), giữ `conversation_id`, và từ đó **mọi lượt chỉ gửi `conversation_id`, không gửi `history`**. Lý do: server là nguồn lịch sử duy nhất (§6.7 xung đột #1), và cách này không cần thêm `conversation_id` vào `ChatResponse` → không đụng swagger/DTO, không thêm gate mới.
- `history` của client chỉ còn đường legacy (khi `conversation_id = 0`) — code FE không đi vào nhánh đó nữa, giữ nguyên BE cho client cũ.
- Scope (category) chọn trên UI được ghi lúc tạo phiên; đổi scope giữa phiên là **thao tác có chủ đích** thì BE đã xử lý (per-message `effective_category_ids`, Đợt 24) — UI hiển thị phạm vi trên từng lượt đã lưu.

**Hành vi UI chốt theo bộ (đối chiếu Open WebUI/Dify ở §2):**

| Vùng | Hành vi | Lý do / repo tham chiếu |
|---|---|---|
| Sidebar phiên | Danh sách newest-first theo `last_message_at`, vô hạn bằng "Tải thêm" (`?before=<id>` keyset), mỗi mục hiện title + thời gian tương đối + số message | Dify/OWUI cùng xếp theo mốc message mới nhất; cursor keyset tránh nhảy bản ghi khi có lượt mới |
| Phiên đang mở | Highlight mục đang xem; mở lại là **nạp `GET /conversations/:id/messages`** (reverse newest-first thành ASC), không đoán lại cites | chip `[^n]` (Đợt 23) phải sống lại đúng số cũ |
| Đổi tên | Inline rename bằng `PATCH`; title tự sinh từ lượt đầu nếu để trống → sau lượt đầu phải **refresh dòng sidebar** để thấy title mới | Dify auto-title |
| Xoá | Confirm dialog (dùng `shared/feedback/confirm-dialog` đã có) rồi `DELETE`; phiên biến mất khỏi sidebar, khung chat trở về "Phiên mới" | xoá mềm, message giữ lại ở DB (Đợt 24) — UI không được nói "đã xoá vĩnh viễn" |
| Đang trả lời | Nút Gửi thành Dừng (đã có); **khoá đổi phiên/xoá khi `sending`** để không mất lượt giữa chừng | tránh ghi dở |
| Rỗng/lỗi | Sidebar rỗng → một dòng hướng dẫn; lỗi tải danh sách → retry inline, không toast đè 5 lần; 404 khi mở phiên bị xoá → toast + về phiên mới | `errorFromApi` đã có sẵn |
| Lặp lại | Bấm chip `[^n]` trong lượt cũ sau khi reload: nguồn lấy từ `cites` đã lưu, không gọi lại search | đã chốt ở §6.7 xung đột #2 |
| Responsive | Sidebar là panel gấp được (`panelOpen` đã có) — màn hẹp tự gập, không che khung chat | giữ hành vi hiện hữu |
| Accessibility | Danh sách phiên là `nav` + nút thật, điều hướng bằng phím; không dùng `id`/`data-*` (sanitizer cắt — bài học đã có memory) | Đợt 23 dùng delegate + textContent |

## 5B. Data phức tạp (mũi chính: chat với Excel)

Repo **chưa có fixture `.xlsx`/`.pptx` nào** (đã kiểm tra bằng glob), và máy chưa cài `openpyxl`/`python-pptx` (đã kiểm tra bằng `python -c import` → ModuleNotFoundError). Nên `loadtest/make_complex_fixtures.py` **tự sinh bằng stdlib** (`zipfile` + XML thô, `inlineStr` để không cần sharedStrings) — chạy lại được, không phụ thuộc mạng. Bộ sinh ra, đặt trong `loadtest/fixtures_complex/`:

| File | Tính chất cần có | Ép hành vi gì |
|---|---|---|
| `bang_luong_q3.xlsx` (nhiều sheet: `Luong`, `Phu_cap`, `Ghi chu`) | số thập phân, % , ngày, ô rỗng, sheet trống, tiêu đề 2 dòng | `list_sheets`/`read_sheet` chọn đúng sheet; hỏi gộp phải đọc ≥2 sheet |
| `hop_dong_thue_mat.xlsx` | cùng tên cột ở 2 sheet, số tiền khác nhau | không được cộng nhầm sheet; phải nêu rõ đọc sheet nào |
| `quy_che_phi_v2.xlsx` vs `quy_che_phi_v1.xlsx` | cùng quy định, mức khác nhau, file có ngày mới hơn | xử lý xung đột/bản mới (prompt đã có luật, chưa từng được test bằng dữ liệu) |
| `bang_ke_toan_multi_sheet.xlsx` | bảng dài > 1 màn, có dòng tổng | trích dẫn phải trỏ sheet+dòng, không được tự đếm |
| `quy_dinh_nhieu_phu_luc.pdf` | Điều có khoản dẫn chiếu sang Phụ lục A/B | mắt xích suy luận nhiều tầng |
| `scan_vua_text_vua_anh.pdf` nếu làm được | nửa text-layer, nửa cần OCR | nhánh OCR + text trộn |

Vì chưa có fixture nào tồn tại, đây là **code sinh dữ liệu mới** — script phải tự kiểm chứng file mình sinh (mở lại bằng `zipfile`, đọc `xl/workbook.xml` đếm sheet) và in bảng để người đọc biết data gồm gì. Điều kiện tiền đề bắt buộc: worker (`go run ./cmd/worker`) phải chạy để extract+index; với user eval mới tinh thì vector của chính user đó được tạo nên `cites` khác rỗng (bịt đúng khoảng hở Đợt 24 — bài học "vector scope theo user_id").

## 5C. Bộ test case + tài liệu demo

`docs_test_case_and_data/testcase/chat_session_uiux_va_demo.md` (tiếng Việt, để demo cho user luôn), gồm:

1. **Bảng hành vi mong đợi** của từng mục trong §5A (input → trạng thái UI → dữ liệu DB).
2. **Test case phiên chat** (đánh số `S-01`…): mở/nạp/đổi tên/xoá/phân trang/404 phiên của người khác/phiên bị xoá giữa lúc đang gõ/hai tab cùng một phiên/không gửi history khi có `conversation_id`.
3. **Test case đa phương tiện phức tạp** (`X-01`…): Excel nhiều sheet, sheet trống, hai file xung đột, phụ lục, số thập phân/percentage/ngày, "cộng dồn cột này sang sheet kia" (bẫy), "file này có mấy sheet" (phải dùng tool, không đoán).
4. **Test case chất lượng câu trả lời chuyên gia** (`Q-01`…) chấm theo rubric §4: BLUF, giải thích thuật ngữ, điều kiện trước phán quyết, không sáo ngữ, độ dài tương xứng.
5. **Kịch bản demo 10 phút** cho user: data nào upload, câu hỏi nào bấm trước, điểm nhấn nào chỉ vào (chip trích dẫn, mở lại phiên cũ, Excel multi-sheet, hai quy định xung đột).
6. Checklist để bạn tự test UI (tôi không mở browser theo ràng buộc đã chốt).

## 5D. File FE sẽ đụng tới (theo pattern đã có trong repo)

| File | Việc |
|---|---|
| `core/chat/chat.models.ts` | thêm model `Conversation` / `ChatMessageSaved` + mapper `toConversation(...)` theo khuôn `toChatAnswer` |
| `core/chat/conversation.service.ts` (mới) | list/create/rename/delete/messages qua `ApiService` (nhân pattern `knowledge.service.ts`), trả domain model chứ không trả DTO |
| `core/chat/chat.service.ts` | `send(...)` nhận thêm `conversationId`; có id thì **không gửi `history`** (server là nguồn duy nhất) |
| `pages/chat/chat.ts` | state phiên (id đang mở, danh sách, cursor "Tải thêm"), nạp lại phiên, rename inline, xoá + confirm, khoá khi `sending`; sau lượt đầu refresh sidebar để ăn title tự sinh |
| `pages/chat/chat.html` / `.scss` | mục phiên trong panel đang có (`panelOpen`), nút đổi tên/xoá, dòng "Tải thêm", empty state, hiển thị `effective_category_ids` trên lượt đã lưu |
| `shared/i18n/vi.ts` + `en.ts` | key mới — bộ dịch type-safe (`TranslationKey`), thiếu key là lỗi build chứ không âm thầm |
| `core/chat/conversation.service.spec.ts` (mới) | vitest cho mapper + các request (HttpTestingController theo khuôn `knowledge.service.spec.ts`) |

Ràng buộc FE đã biết từ các đợt trước: `npm run lint` (ESLint 9 flat, angular-eslint + a11y — template phải có `aria-*` cho nút icon), **không** tự format bằng lint (format bằng `npx prettier --write`), budget component style trong `angular.json` (chat.scss đã 584 dòng → thêm style phải gọn, ưu tiên reuse token có sẵn), sanitizer cắt `id`/`data-*` nên thôi dùng delegate + textContent như Đợt 23.

## 6. Cách kiểm chứng

1. `go build ./...` && `go vet ./...` && `golangci-lint run ./...` && `go test ./...` (prompt là file, không liên quan build; chạy đủ để chắc không hỏng gì).
2. `python -m py_compile loadtest/expert_eval.py` + chạy `--help`.
3. **Smoke 1 lô** (3 câu, 1 provider) xác nhận: bộ nhịp chờ đúng cửa sổ, `conversation_id` được tạo, message vào DB.
4. **Chạy thật xoay 3 provider**, `--max 12 --batch-size 4`: bảng verdict per provider + số câu có `cites` khác rỗng.
5. `psql` (read-only qua `docker exec`): đối chiếu hàng `messages` của các phiên eval — `provider` đúng per message, `cites` decode ra mảng object, `effective_category_ids` khớp scope.
6. **Bằng chứng cho "chuyên gia":** với cùng 3 câu, in trả lời trước/sau khi đổi prompt (trước = transcript Đợt 24 còn giữ trong DB nếu cùng tài liệu, nếu không thì chạy lại câu đó bằng prompt cũ qua git stash) → ghi vào `## Kết quả` kèm nhận xét theo rubric.
7. Dọn dữ liệu: xoá các phiên eval + bài upload của eval (qua API purge/delete, có ghi lệnh) hoặc nêu rõ để lại kèm id.

## 7. Tiêu chí hoàn thành

- [x] Bảng đối chiếu OSS vào `RAG_RESEARCH.md` §6.7.1 (mỗi dòng có nguồn đã đọc).
- [x] `prompts/chat_system.md` mục 10 rewritten, không mâu thuẫn 12 ví dụ, không đổi số thứ tự mục khác.
- [x] `loadtest/expert_eval.py` chạy được theo lô, xoay 9router/nvidia/openrouter, tôn trọng trần 40 req/phút, có bằng chứng log chứng minh nhịp.
- [x] Rubric file + verdict 3 lớp in ra được.
- [x] `loadtest/make_complex_fixtures.py` sinh được `.xlsx` nhiều sheet (tự kiểm chứng bằng cách mở lại bằng `zipfile` + đếm sheet trong `xl/workbook.xml`).
- [x] FE: sidebar phiên chạy theo đúng bảng hành vi §5A; `npm run lint` + `npm test` + `npm run build` xanh (tôi **không** tự mở browser test UI — người dùng tự test theo checklist).
- [x] Tài liệu demo `docs_test_case_and_data/testcase/chat_session_uiux_va_demo.md` có đủ: bảng hành vi · test case S/X/Q · kịch bản demo 10 phút · checklist UI.
- [x] **`cites` khác rỗng được chứng minh live** (nợ của Đợt 24) hoặc báo rõ vì sao vẫn chưa.
- [x] Verify Go xanh, `codegraph sync` (nếu sửa Go), `swag init` chỉ nếu đổi annotation.
- [x] `## Kết quả` điền + archive plan + thêm dòng `plans/README.md`, commit message **tiếng Anh**.

## 8. Rủi ro

| Rủi ro | Xử lý |
|---|---|
| Trần 40 req/phút tính theo **lượt LLM**, không theo lượt `/chat`; một câu ăn 8-10 lượt → vượt trần rất dễ | `batch-size 4` mặc định + docstring giải thích; `rate_limit_rpm` server-side là lớp 2; 429 đã có `retryTransport` (tối đa 4 lần, tôn trọng `Retry-After`) |
| Model free (`nemotron-...:free`) thỉnh thoảng trả answer rỗng → đọc nhầm là "kém chuyên gia" | Đếm riêng verdict `EMPTY_ANSWER`, không gộp vào FAIL; `run()` đã có 1 retry empty-answer |
| 9router hết credit (402) giữa lô | Tắt fallback theo thiết kế §5.2 nên câu đó ERROR + report biết; chạy lại phần còn bằng `--providers` khác |
| Sửa prompt làm hỏng quy tắc căn cứ đã kiểm chứng (Đợt 22/24) | Chỉ thêm mục 10, không đụng mục 0/1/2/8/11; Layer 0 kiểm tra marker `[^n]` ↔ `cites` trên từng câu chạy thật |
| Đổi prompt làm chất lượng **tụt** ở provider yếu | So per-provider, không so trung vị; nếu tụt thì giữ prompt cũ cho đợt sau và ghi bằng chứng |
| Eval tốn thời gian (mỗi câu 60-300s) | `--max` mặc định nhỏ (12), chạy lô nền và poll, không block phiên làm việc |

## 9. Kết quả

### 9.1 Batch thật, xoay 3 provider (chạy ngày 2026-10-27, `--rpm 30 --batch-size 4 --concurrency 2 --max 6`)

| Case | Provider | Verdict lúc chạy | `l1.factual` chấm lại offline | `cites` | `l2.shape` | giây |
|---|---|---|---|---|---|---|
| X-01 | 9router | PASS | PASS | 5 | 5/6 | 116.0 |
| X-02 | nvidia | PASS | PASS | 10 | 6/6 | 146.8 |
| X-03 | openrouter | **FAIL1** | **PASS** (sửa bộ chấm, xem §9.2) | 10 | 6/6 | 178.1 |
| X-04 | 9router | PASS | PASS | 11 | 6/6 | 166.6 |
| X-05 | nvidia | PASS | PASS | 10 | 6/6 | 242.5 |
| X-06 | openrouter | PASS | PASS | 10 | 6/6 | 33.4 |

Bảng tổng hợp harness in ra (`n pass fail0 fail1 err cites>0 shape unsrc avg_s`):

```text
9router     2  2  0  0  0  2  0.92   8  141.3
nvidia      2  2  0  0  0  2  1.00  11  194.7
openrouter  2  1  0  1  0  2  1.00   1  105.8
```

- **`cites` khác rỗng trên 6/6 câu — nợ Đợt 24 đã trả.** psql đối chiếu: 6 phiên `id` 28–33 (`user_id=103`, `provider` đúng từng phiên), mỗi phiên đúng 2 hàng `messages` (user rồi assistant), `cite_rows` 5–11, hàng `user` có `cites` rỗng (đúng thiết kế), `effective_category_ids = [130]` trên cả hai hàng. Payload `cites` decode ra mảng object `{n, snippet, page_num, article_id, chunk_index}` — snippet là **text bảng đã chuyển đổi** (`## Sheet: Phu_cap` + các dòng `|`), nghĩa là chuỗi Excel đi hết đường ingest → chunk → embed → retrieve.
- **Không câu nào FAIL0**: không answer rỗng, không dangling marker, không provider drift, không thiếu `question_type`. Lớp kỹ thuật sạch trên cả ba provider.
- **`unsrc` (số không truy ra snippet) là chỉ báo, đã đối chiếu thủ công từng số** với `loadtest/make_complex_fixtures.py`: `1.200.000` (NV02 phụ cấp độc hại), `2.000.000` (NV04 phụ cấp trách nhiệm), `180.000` (lương ngày NV03), `21.5`/`22.5` (ngày công) đều **có thật trong fixture** nhưng nằm ngoài cửa sổ snippet được cite; `25.370.000 + 4.600.000 = 29.970.000`, `29.970.000 − 2.623.500 = 27.346.500`, `29.970.000 + 600.000 = 30.570.000` là phép tính hợp lệ từ số có nguồn. **Không phát hiện số bịa.**
- Bộ nhịp: `governor nghỉ 0 lần trong 537s` — với 6 câu / 2 luồng thì 30 req/phút không bao giờ chạm trần; cửa sổ chỉ siết khi tăng `--concurrency` hoặc chạy hết 29 case.

### 9.2 Batch thật lộ ra hai lỗi — một của model, một của bộ chấm

1. **Model lộ biến thể nội bộ tool** (X-01, 9router): câu trả lời kết bằng "(Theo kết quả đọc cấu trúc file:, sheet_count = 3)" và **không có marker nào** trong 190 ký tự. `PLUMBING_RE` cũ không có `sheet_count` nên `no_plumbing_leak` vẫn cho qua — đã bổ sung `sheet_count|row_count|page_count|column_count|top_k|point_id|kb_id`. Chấm lại thì X-01 trượt đúng 2 tiêu chí (`no_plumbing_leak` + `citation_density`), phản ánh đúng chất lượng thật.
2. **Bộ chấm báo oan** (X-03, openrouter): đáp án mong đợi ghi `22,5` (phẩy thập phân kiểu VN), model ghi `22.5` → `FAIL:22,5`, trong khi nội dung trả lời **đúng hẳn**: nêu ngưỡng 22 ngày công, NV01 22.5 → 800.000, NV05 20 (nghỉ 5 ngày) → 600.000. Đã thêm `num_variants()` vào `loadtest/e2e_testcases.py` (phẩy↔chấm ở phần thập phân, `.`↔`,` ở hàng nghìn), `grade()` và `check_layer1()` cùng dùng tập biến thể; selftest tăng **11 → 14 mục**, ba mục mới khoá đúng hai lỗi trên. Chấm lại offline 6 lượt đã lưu: **6/6 PASS factual**.

### 9.3 Hành vi "chuyên gia" quan sát được trong answer thật

- **BLUF + nêu điều kiện trước phán quyết** (X-03): mở đầu bằng nguyên nhân "làm việc 22.5 ngày công, đạt điều kiện 'từ 22 ngày công trở lên' nên được 800.000", chốt "Nguyên nhân chênh lệch là ngưỡng 22 ngày công".
- **Không chọn bừa khi dữ liệu mâu thuẫn** (X-04): chỉ ra sheet `Luong` để trống ô Phụ cấp của NV03 (thực nhận 3.870.000 đúng bằng lương cơ bản) trong khi `Phu_cap` ghi 600.000, và nêu cả hai con số thay vì trả lời "có"/"không".
- **Tự suy luận và tự gắn nhãn** (X-06): sau khi trích "ngày 05 của tháng kế tiếp" thì suy ra kỳ kết thúc 30/09/2025 → hạn chốt 05/10/2025, và **ghi rõ** "(suy luận từ ngày kết thúc kỳ và quy định ...)" — đúng ý mục 10 prompt: phân biệt dữ kiện có nguồn với suy luận.
- **Trả lời đủ biên độ** (X-05): nêu cả hai bậc 10% và 5% rồi đối chiếu từng nhân viên, không nói chung chung.
- **Chưa làm được như plan đề ra:** mục kiểm chứng số 6 (in trước/sau khi đổi prompt để so sánh) **không thực hiện** — Đợt 24 không giữ transcript cùng bộ tài liệu này nên không có baseline sạch. Bằng chứng ở trên là one-sided; nếu muốn con số trước/sau thì Đợt 26 chạy lại cùng 6 câu với `git stash` prompt cũ.

### 9.4 FE (UI/UX phiên chat)

- Panel tiện ích ở **trái** card chat (ưu tiên đã ghi nhận), nút gập góc trên-trái; ≤1023px panel thành thanh ngang và nút gập sang phải. Lazy session (chưa hỏi chưa tạo phiên), rename/delete, khoá scope + "Suy nghĩ sâu" khi phiên đã có lượt.
- `npm run lint` sạch; vitest **22 files / 127 tests** xanh; `npm run build` không còn WARNING.
- Budget `anyComponentStyle` nâng **10/12kB → 14/16kB** theo phép của chủ project ("chỉ cần phù hợp, hợp lý cứ nâng budget") vì `chat.scss` 11.65kB; precedent của repo là 8→9→10kB. Không tách component.

### 9.5 Nợ chuyển tiếp (ghi rõ, không quên)

- Còn **23/29 case** complex chưa chạy thật (mỗi câu 33–242s). Chạy tiếp: `--skip-upload --category-id 130 --max 0` (lệnh mẫu ở `chat_session_uiux_va_demo.md` §5).
- Dữ liệu eval **để lại** phục vụ demo: user `id=103`, danh mục `130`, bài `73–79` (page_count 1/1/1/1/1/3/2), phiên `28–33`. Muốn dọn: `DELETE /api/v1/articles/:id?purge=true` từng bài + `DELETE /api/v1/conversations/:id`.
- Chưa lưu `reasoning`; F5 không nhớ phiên đang mở; window lịch sử vẫn theo **số lượt** (12) chứ chưa theo ngân sách token (LlamaIndex làm khác mình ở đây — đã ghi §6.7.1).
- `usage` per message (Dify và Open WebUI đều có) — **defer**, thuộc S3 vì cần số token từ Eino.
- Chart: đã chốt thiết kế ở `RAG_RESEARCH.md` §6.9 (tool `chart_from_cite`, server dựng spec từ chunk được cite), Đợt 26 ghi plan trước khi code, case X-30…X-35 đã nằm trong `chat_session_uiux_va_demo.md`.

### 9.6 Verify

- `go build ./...` + `go vet ./...` + `go test ./...` + `golangci-lint run ./...` (chạy ngày 2026-10-27): **0 issues**, mọi package `ok` — đợt này không sửa Go source nên chạy để chắc không hỏng gì.
- Python: `python -m py_compile expert_eval.py e2e_testcases.py` sạch, `--selftest` đạt 14/14.
- FE: lint + 127 tests + build xanh (bằng chứng ở §9.4).
- `codegraph sync`: không cần — đợt này không sửa Go source.

### 9.7 Commit của đợt (message tiếng Anh toàn bộ)

`e444c22`/`a9d5a1d` plan · `d65f2a1` prompt mục 10 · `b79d0f2` fixtures complex + test extractor Go + harness · `8df1b85` + `7dac50e` + `4659f32` FE · `cffaabe` rubric + test case + demo · `64e5bda` sửa schema claims + 29 case chạy được · `f0e7ca3` Open WebUI hai tầng · commit cuối: sửa bộ chấm (decimal + plumbing) và điền kết quả này.
