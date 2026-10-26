# Đợt 25 — Trả lời như chuyên gia + eval đa provider theo lô (9router / nvidia / openrouter)

Ngày: 2026-10-26 · Trạng thái: kế hoạch (chưa code) · Nối: Đợt 24 (`archive/2026-10/` — session chat lưu DB, S1+S4)

## 1. Yêu cầu đã nhận

Người dùng yêu cầu 4 việc, chốt lại thành một đợt:

1. **Nghiên cứu** cách các repo OSS nổi tiếng làm đúng tính năng này (lưu phiên chat, lịch sử, trích dẫn theo message) rồi đối chiếu với thiết kế mình đang có.
2. **Test bằng các LLM trong config**, xoay quanh **9router**, **nvidia**, **openrouter** (`configs/config.local.json` đã đủ key cho cả ba).
3. Hướng tới **AI trả lời như một chuyên gia** — từ câu văn tới cách giải thích.
4. **Test API theo lô**, vì giới hạn phía provider là **40 request/phút**.

Ràng buộc thêm: mọi commit message bằng **tiếng Anh**.

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
Harness chạy lô, **import lại** helper của `e2e_testcases.py` (`login`, `auth`, `ask_chat`, `parse_cases`, `num_requirements`, `norm_vi`) — không fork logic upload/index-wait/rotation đã có.

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
- **Không bao giờ in API key:** key đọc từ `configs/config.local.json` (file gitignored) chỉ khi cần; kết quả chỉ ghi tên provider/model.

### 5.3 `docs_test_case_and_data/testcase/expert_answer_rubric.md` (mới)
Rubric §5 dưới dạng checklist để người đọc chấm nhanh 3-5 câu tiêu biểu per provider, và để sau này biến thành judge tự động.

### 5.4 `configs/config.local.json` (KHÔNG commit — gitignored)
Đặt `rate_limit_rpm: 35` cho `9router`, `nvidia`, `openrouter` để token-bucket phía server (`src/agent/eino/model.go:newRateLimitTransport`) thành lớp phòng thủ thứ hai; `retryTransport` đã tự xử lý 429. Ghi lại trong `## Kết quả` giá trị đã đặt (không ghi key).

### 5.5 `RAG_RESEARCH.md`
Thêm mục **§6.7.1 Đối chiếu OSS (Đợt 25)** với bảng phát hiện §2 + đường dẫn nguồn, để lần sau làm S2/S5 không phải đọc lại 4 repo.

**Không làm đợt này** (ghi rõ để khỏi lan man): `usage` per message; allowlist trường cites; `context_summary` cho S2; Memory-style store; LLM-as-judge; FE sidebar lịch sử (vẫn là Đợt 25b trong backlog).

## 6. Cách kiểm chứng

1. `go build ./...` && `go vet ./...` && `golangci-lint run ./...` && `go test ./...` (prompt là file, không liên quan build; chạy đủ để chắc không hỏng gì).
2. `python -m py_compile loadtest/expert_eval.py` + chạy `--help`.
3. **Smoke 1 lô** (3 câu, 1 provider) xác nhận: bộ nhịp chờ đúng cửa sổ, `conversation_id` được tạo, message vào DB.
4. **Chạy thật xoay 3 provider**, `--max 12 --batch-size 4`: bảng verdict per provider + số câu có `cites` khác rỗng.
5. `psql` (read-only qua `docker exec`): đối chiếu hàng `messages` của các phiên eval — `provider` đúng per message, `cites` decode ra mảng object, `effective_category_ids` khớp scope.
6. **Bằng chứng cho "chuyên gia":** với cùng 3 câu, in trả lời trước/sau khi đổi prompt (trước = transcript Đợt 24 còn giữ trong DB nếu cùng tài liệu, nếu không thì chạy lại câu đó bằng prompt cũ qua git stash) → ghi vào `## Kết quả` kèm nhận xét theo rubric.
7. Dọn dữ liệu: xoá các phiên eval + bài upload của eval (qua API purge/delete, có ghi lệnh) hoặc nêu rõ để lại kèm id.

## 7. Tiêu chí hoàn thành

- [ ] Bảng đối chiếu OSS vào `RAG_RESEARCH.md` §6.7.1 (mỗi dòng có nguồn đã đọc).
- [ ] `prompts/chat_system.md` mục 10 rewritten, không mâu thuẫn 12 ví dụ, không đổi số thứ tự mục khác.
- [ ] `loadtest/expert_eval.py` chạy được theo lô, xoay 9router/nvidia/openrouter, tôn trọng trần 40 req/phút, có bằng chứng log chứng minh nhịp.
- [ ] Rubric file + verdict 3 lớp in ra được.
- [ ] **`cites` khác rỗng được chứng minh live** (nợ của Đợt 24) hoặc báo rõ vì sao vẫn chưa.
- [ ] Verify Go xanh, `codegraph sync` (nếu sửa Go), `swag init` chỉ nếu đổi annotation.
- [ ] `## Kết quả` điền + archive plan + thêm dòng `plans/README.md`, commit message **tiếng Anh**.

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

*(điền khi xong)*
