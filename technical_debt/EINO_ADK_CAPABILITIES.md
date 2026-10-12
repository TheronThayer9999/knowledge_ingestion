# Eino ADK — Năng lực framework cho agent RAG

> Khảo sát trên `github.com/cloudwego/eino v0.9.21` (go.mod hiện dùng). Ghi
> lại để ra quyết định tích hợp — không phải摸索 lại từ đầu.

## 1. Deep search / lập kế hoạch — `adk/prebuilt/`

| Prebuilt | Làm gì | Áp dụng cho ta |
|---|---|---|
| `deep` | DeepAgent kiểu Claude Code: tool `task` spawn **subagent ngắn hạn** cho từng nhiệm vụ con (song song, cách ly context — task tốn token nặng không phình thread chính), `write_todos` theo dõi kế hoạch, có checkpoint/resume | Câu "liệt kê/tóm tắt toàn bộ" — giao mỗi file 1 subagent đọc riêng, context không tràn câu trả lời chính. **Nặng nhất, làm sau** |
| `planexecute` | Planner → Executor → **Replanner** (chốt plan → chạy → đánh giá lại) | Quy trình nhiều bước nếu cần |
| `supervisor` | Orchestrator điều phối nhiều subagent | Nhiều nguồn đa luồng |

Hiện tại: prompt rule "liệt kê → 2 lượt liên tiếp không ra Điều mới thì chốt
(tối đa 4)" + retry Go. Nếu vụ file 500 trang là thật thì chuyển `deep`.

## 2. Retrieval sâu — `flow/retriever/`

- **`multiquery`**: rewrite 1 câu thành nhiều biến thể → tìm nhiều hướng, tăng
  recall. Thay cách "đổi từ khóa tìm tiếp" bằng prompt (đang rule §11).
- **`router`**: gộp kết quả nhiều retriever + rank lại (ta tự RRF trong
  `src/agent/retrieval/hybrid_search.go`).
- **`parent`**: map chunk con về **parent document** — cite trả đoạn đủ ngữ
  cảnh thay vì mảnh.

## 3. Memory / lịch sử — `adk/middlewares/`

- **`summarization`**: middleware tự **tóm tắt history khi vượt ngưỡng token**
  (`ContextTokens` / `ContextMessages` trong config) thay cách cắt cứng
  `maxHistoryTurns = 6` (`src/agent/eino/agent.go`) — hội thoại dài không mất
  ngữ cảnh đầu, không phình context. Có failover model riêng cho lượt tóm.
- **`runctx` + `CheckPointStore`**: session state gob, **checkpoint/resume**
  giữa chừng (turn dài timeout thì resume tiếp). Runner: `WithCheckPointID`.
- Middleware khác: `skill` (nạp skill theo lệnh), `plantask`, `dynamictool`
  (đổi tool set runtime), `reduction` (gom message), `patchtoolcalls`.

**Không có** package memory "facts vĩnh viễn" kiểu Mem0 — memory của Eino =
history + summarization + checkpoint.

## 4. Đối chiếu code hiện tại → lộ trình

| Tính năng | Code hiện tại | Framework | Ưu tiên |
|---|---|---|---|
| Lịch sử hội thoại | **✅ đã làm**: `summarization` middleware (`src/agent/eino/summarize.go`), trigger 16k token/60 msg, `keepRunTailFinalize` giữ đuôi tool của lượt hiện tại; `maxHistoryTurns` 6→40 | `summarization` middleware | 1 — xong |
| Tăng recall | over-fetch 3× + prompt "đổi từ khóa" | `multiquery` rewrite | 2 |
| Deep search liệt kê | prompt rule + retry Go | `prebuilt/deep` subagent per-file | 3 (chỉ nếu file 500 trang thật) |
| Resume khi timeout | mất cả lượt | `CheckPointStore` | 4 |

### Ghi chú tích hợp

- `summarization` gắn tại `NewChatAgent` (`src/agent/eino/agent.go`) — cùng
  chỗ dựng `ChatModelAgentConfig`, model tóm dùng chung client (khác model
  thì config `Failover` riêng).
- `multiquery` thay `HybridSearcher` ở tool `knowledge_search` hoặc ngay trong
  `retrieval` — giữ RRFfuse hiện tại, chỉ thêm bước rewrite trước đó.
- Ngưỡng summarization nên tính theo model: OpenRouter free context thường
  8-128k, Ollama cloud tùy model — đặt `ContextTokens` bảo thủ (~60% window).
