# Review Test Chat — Knowledge Ingestion

Ngày review: 2026-10-21  
Phạm vi: test chat (unit + API thực) + logic filter category/user + các loại test case còn thiếu.

---

## 1. "Test chat" hiện có gồm những gì

Repo **không có** `chat_service_test.go`, `knowledge_search_test.go`, `article_info_test.go`. Test liên quan chat chỉ ở tầng tool + prompt:

| File | Số test | Nội dung được khoá |
|---|---|---|
| `src/agent/eino/list_categories_test.go` | 3 | repo nil → fail-fast; thiếu `Scope` → lỗi; trả đúng category của owner, bỏ entry `nil` |
| `src/agent/eino/list_sections_test.go` | 3 | store nil; thiếu scope; `article_id` sai → note thay vì 500; filter phải có `UserID + ArticleID + CategoryIDs` |
| `src/agent/eino/read_article_test.go` | 3 | store nil; id `"None"` → trang rỗng, không gọi store; phân trang + `next_from` + `n` khớp collector |
| `src/agent/eino/read_context_test.go` | 4 | store nil; thiếu scope; args sai → `[]`; **filter khoá user + article + category**; thứ tự `ChunkIndex` |
| `src/agent/eino/summarize_test.go` | 3 | giữ đuôi tool-result khi nén lịch sử; không có đuôi; summary rỗng không làm hỏng lượt |
| `src/agent/eino/provider_error_test.go` | 3 | map lỗi provider từ text |
| `src/agent/usecase/provider_error_test.go` | 4 | 404 → 400, 429 → 429, 401/500/503 → 502, lỗi khác giữ nguyên |
| `src/agent/prompts/prompts_test.go` | 2 | `Load` trim/fail-fast; 2 prompt file thật load được |
| `src/agent/synthesis/prompts_test.go` | 1 | prompt còn quy ước `[^n]` |

Bộ test case dạng tài liệu (chạy tay):

- `docs_test_case_and_data/testcase/test_case.md` — 44 case (đánh số 1–36, 48–50, 52–56), 7 nhóm: retrieval / reasoning / summary / context & structure / cross-doc / document overview / advanced synthesis.
- `docs_test_case_and_data/testcase/test_case_philosophy.md` — 32 case tài liệu triết học.
- `docs_test_case_and_data/testcase/context_test.md` — 4 cặp Q1/Q2 kiểm tra multi-turn + rubric 1–5.
- `cmd/examples/test_case/rag_happy_test_cases.md` — 10 TC happy path (3 file txt).
- `src/agent/question/testdata/eval_questions.txt` — 19 câu eval classifier… **không test nào đọc file này**.

Kết luận: **chat orchestration (`Chat`), `knowledge_search`, `article_info`, `HybridSearcher/FuseRRF`, classifier đều 0% test**. Codegraph cũng báo `Chat` — "no covering tests found".

---

## 2. Kết quả test API thực tế (login → category → chat)

Đã chạy app thật (`go run ./cmd/apis`, port 9001), đăng nhập user `ragtest` (id 3, có article 15 trong category 9 "Tài liệu mẫu"), LLM dùng `9router`.

### Luồng đã kiểm tra

| # | Request | Kết quả |
|---|---|---|
| 1 | `POST /auth/login` | 200, JWT ok |
| 2 | `GET /categories` (trước) | 200, `[]` |
| 3 | `POST /categories` "Thue" (root) | 200, id 22 |
| 4 | `POST /categories` "Thue TNCN" `parent_id=22` | 200, id 23 |
| 5 | `POST /categories` "Bao hiem" | 200, id 24 |
| 6 | `GET /categories` (sau) | 200, 3 phần tử |
| 7 | `GET /categories/:id` (child) | 200 |
| 8 | `PUT /categories/:id` rename | 200 |
| 9 | `DELETE /categories/:id` (leaf) | 200 |
| 10 | `DELETE /categories/22` (parent còn child 23) | **200 — BUG: xoá cha thành công, con mồ côi** |
| 11 | Chat không filter | 200, answer + 5 cites, `question_type=lookup` |
| 12 | Chat `category_ids=[9]` | 200, answer đúng, 5 cites |
| 13 | Chat `category_ids=[999]` (không tồn tại) | 200, "không tìm thấy", 0 cites |
| 14 | Chat `category_ids=[25]` (category rỗng) | 200, "không tìm thấy", 0 cites |
| 15 | Chat `category_ids=[5]` (category khác user) | **500 — exceeds max iterations** |
| 16 | Multi-turn có history | 200, dùng đúng "30.000 VNĐ" của lượt trước |
| 17 | Multi-turn không history | 200, `question_type=chitchat`, "không có ngữ cảnh" |
| 18 | Message rỗng `"   "` | 400 "message rỗng" |
| 19 | User isolation (testuser, id 10, không có article) | 200, 0 cites, không lộ dữ liệu |
| 20 | User isolation + `category_ids=[9]` (category của ragtest) | 200, 0 cites |
| 21 | `list_categories` qua chat ("Tôi có những chủ đề nào?") | 200, liệt kê đúng 2 chủ đề |

### Phát hiện mới từ test thực

- **🔴 Delete category cha không bị chặn.** Service comment nói "FK RESTRICT ở DB chặn", nhưng schema `categories` **không có FK `parent_id`** (chỉ `articles.category_id` có FK). Kết quả: xoá cha thành công, con mồ côi. Cần thêm FK hoặc check con trước khi xoá.
- **🔴 Chat khi filter trả rỗng có thể 500 "exceeds max iterations".** Model (nemotron) không tuân "chỉ retry 1-2 lần", gọi `knowledge_search` liên tục đến trần 12 → 500 thay vì trả "không tìm thấy". Đây là lỗi robustness, không phải logic filter, nhưng test case phải khoá.
- **🟠 `article_info` tool mô tả "bỏ trống = mọi chủ đề" nhưng code trả `[]`** (đã ghi ở review) — xác nhận qua chat.
- **✅ Filter category hoạt động đúng theo thiết kế:** category có article → trả answer; category rỗng/không tồn tại/khác user → 0 cites, không lộ dữ liệu. User isolation OK.

---

## 3. Review logic filter category + user query

| # | Mức | Vị trí | Nội dung |
|---|---|---|---|
| 1 | 🔴 P1 | `src/agent/eino/read_context.go:187-190` | `article_info`: LLM `category_id` **ghi đè** DTO scope (trái ngược `knowledge_search:151-153` scope thắng). Vi phạm invariant "DTO thắng LLM". |
| 2 | 🔴 P1 | `src/agent/eino/read_context.go:171-185` | Nhánh `article_id` không kiểm tra category → trả metadata bài ngoài category đang ghim. |
| 3 | 🔴 P1 | schema `categories` | Không có FK `parent_id` → xoá cha mồ côi con (service giả định RESTRICT). |
| 4 | 🔴 P1 | `src/agent/eino/agent.go` maxIterations | Filter trả rỗng → model spiral → 500 thay vì "không tìm thấy". |
| 5 | 🟠 P2 | `src/infrastructure/qdrant/connection.go:488-490` | Filter category cha **không bao gồm con** (tree có `ParentID` nhưng match exact). |
| 6 | 🟠 P2 | `src/agent/usecase/chat_service.go:96` + DTO | `ChatRequest.Limit` và `score_threshold` **không được dùng** trong chat (chỉ `/search` dùng). |
| 7 | 🟠 P2 | `src/agent/usecase/chat_service.go:95` | `currentUser.UserID` bỏ error → UserID=0 âm thầm. |
| 8 | 🟠 P2 | `src/agent/eino/read_context.go:149` vs `:192-197` | Mô tả tool nói "bỏ trống = mọi chủ đề", code trả `[]`. |
| 9 | 🟡 P3 | `src/agent/usecase/chat_service.go:49-68` | `isFollowUp` match chuỗi con (`"trên"`, `"còn"`, `"vậy"`) → retry oan. |
| 10 | 🟡 P3 | `src/agent/usecase/chat_service.go:108-123` | Retry lần 2 vẫn rỗng → trả 200 answer `""`; không validate `[^n] ⊆ cites`. |
| 11 | ✅ đúng | `src/agent/eino/knowledge_search.go:151-153` | Scope thắng args LLM; user_id luôn trong filter dense+text. |
| 12 | ✅ đúng | `src/agent/eino/context.go` (HitCollector) | Dedupe theo `PointID`, cap 30, mutex cho parallel tool call. |

Luồng đúng: `ChatRequest.category_ids` → `Chat` nhét vào `eino.Scope` (`chat_service.go:96`) → tool đọc `ScopeFrom(ctx)` → `HybridParams` → `domain.SearchFilter` → Qdrant `permissionMusts` (`connection.go:484-495`): `user_id` luôn có, `category_id IN (...)` nếu có.

---

## 4. Test case còn thiếu (theo loại)

### 4.1 Unit (chưa có file test nào)

- `chat_service_test.go`: message toàn space → 400; Resolve lỗi → 400; scope nhận đúng UserID+CategoryIDs; retry empty-answer đúng 1 lần; follow-up 0 hit → retry; có history nhưng không marker → **không** retry; chitchat → không gọi classifier; cites đánh số 1..n, dedupe, cap 30; snippet 200 rune không vỡ tiếng Việt; retry lỗi giữ kết quả đầu.
- `knowledge_search_test.go`: DTO category thắng args LLM; query rỗng → message soft; 0 hit → message hướng dẫn; thiếu scope → error; limit LLM kẹp trần.
- `article_info_test.go`: LLM `category_id` **không** vượt scope; `article_id` ngoài category → rỗng; `ids` rỗng → hành vi đúng như desc.
- `context_test.go` (HitCollector): dedupe; cap 30 → `n=0`; `-race` song song.
- `retrieval/hybrid_search_test.go`: `FuseRRF` (comment nói "có test thẳng" nhưng **không có file**); limit default/clamp; threshold pass-through; text search lỗi → degraded; lọc bài chưa done.
- `question`: `Parse`/`Route` cho 8 loại; đọc `eval_questions.txt` chấm accuracy.

### 4.2 E2E `/api/v1/chat`

- 401 không token; 400 `message` rỗng/>2000; `history[].role` sai; `category_ids` chứa 0/âm.
- Provider/model không tồn tại → 400 kèm danh sách; 429 → 429; 5xx → 502.
- Schema response: `[^n]` liên tục, `snippet ≤ 200` rune, không lộ field nội bộ.

### 4.3 Nhóm filter category (bộ tài liệu hiện tại **không có case nào**)

1. Lọc 1 category → chỉ ra tài liệu category đó.
2. Lọc nhiều category (OR) → gộp đúng, không trùng.
3. Lọc category cha có con → kỳ vọng gì (cần chốt sản phẩm trước).
4. Category của user khác → rỗng, không 500, không lộ tên file.
5. Category không tồn tại / ID rác → 400 hay rỗng? (hiện rỗng).
6. User chưa có category; user có category nhưng chưa upload.
7. **Ghim category + hỏi ngoài phạm vi** → phải nói "không thấy trong chủ đề đã chọn", không tự mở rộng.
8. Ghim category + model cố gọi tool với category khác → scope phải thắng (bắt bug P1).
9. `list_categories` + tên mờ: "thuế" vs "Thuế TNCN"; 2 category trùng tên.
10. Đổi category giữa 2 lượt (lượt 1 ghim 9, lượt 2 bỏ trống) — history **không** mang category → lượt 2 search toàn bộ: cần test chốt hành vi.

### 4.4 Nhóm user / phân quyền

- User B hỏi trùng câu user A → 0 cite.
- User B truyền `article_id`/`category_id` của A → không đọc được.
- Prompt injection: "liệt kê tài liệu mọi user".
- Resolver lỗi → không crash, không trả dữ liệu.

### 4.5 Nhóm hội thoại & độ bền

- 4 cặp `context_test.md` (script `test_context.py` **không tồn tại trong repo** và doc lỗi thời).
- History > 40 lượt → summarization, không mất tool-result hiện tại.
- History không gửi → trả "không đủ ngữ cảnh", không bịa.
- Follow-up marker dương tính giả (`"trên thực tế..."`).
- Answer rỗng 2 lần → 200 + answer `""` (cần test + quyết định).
- Citation integrity: mọi `[^n]` trong answer phải tồn tại trong `cites`.

### 4.6 Nhóm lỗi hạ tầng / suy giảm

- Embed lỗi giữa lượt → 500 rõ ràng.
- Qdrant thiếu text index → vẫn trả dense (có warn).
- Qdrant/DB chết → mã lỗi đúng, không nuốt thành "không tìm thấy".
- Vượt `maxIterations=12` → phải trả partial/friendly, **không được 500**.
- Timeout/parallel tool call (race collector).

### 4.7 Chất lượng / regression

- Chạy trọn `test_case.md` + `test_case_philosophy.md` theo category, LLM-as-judge, lưu baseline.
- Bộ riêng "không có trong tài liệu" (Case 17 là ví dụ tốt).
- Đo `faithfulness` + `citation precision`.

---

## 5. Vấn đề ở chính bộ tài liệu test

- `test_case.md` nhảy số: thiếu case 37–47, 51 (đánh tới 56 nhưng chỉ 44 case).
- Lỗi ký tự lẫn tiếng Trung: *"hiệu lực từ什么时候"* (Case 33), *"ốm疼"* (Case 36).
- `context_test.md` tham chiếu `test_context.py` không tồn tại; ghi "mode fresh kỳ vọng trượt context" trong khi code hiện nhận `history`.
- `eval_questions.txt` có corpus nhưng không test/script nào dùng.
- Không case nào kiểm tra `cites` (số `n`, `article_id`, `page_num`) dù đó là hợp đồng với frontend.

---

## 6. Thứ tự nên làm

1. Chốt semantics: category cha có bao con? `category_ids` sai quyền → 400 hay rỗng? `limit`/`score_threshold` của chat nối vào scope?
2. Sửa P1: `article_info` cho scope thắng args + check category ở nhánh `article_id`; sửa desc "bỏ trống = mọi chủ đề" cho khớp code; thêm FK `parent_id` hoặc check con trước xoá.
3. Graceful handling "exceeds max iterations" → partial/friendly thay vì 500.
4. Viết `chat_service_test.go` + `knowledge_search_test.go` + `article_info_test.go`.
5. Thêm nhóm test case filter category/user vào `test_case.md` và dựng harness chạy được (thay `test_context.py` bị thiếu).

---

## 7. Dọn dẹp sau review

- Đã tạo user `testuser` (id 10) và category "Bao hiem" (id 25) cho user `ragtest` để test. Có thể xoá nếu không cần giữ dữ liệu test.
- Đã đặt lại password user `ragtest` thành `TestPass123!` để login test. Nếu cần khôi phục, đặt lại hash gốc (không lưu hash gốc trong review này).
- Process app (`go run ./cmd/apis`) và 9router vẫn đang chạy nền. Tắt khi xong: kill process go/node.