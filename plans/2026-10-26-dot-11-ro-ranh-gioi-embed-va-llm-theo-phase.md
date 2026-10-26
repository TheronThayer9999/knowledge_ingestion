# Đợt 11 — Rõ ranh giới "gọi model" theo từng phase

## Bối cảnh

Hai câu hỏi của người dùng được trả lời bằng số liệu thật trước khi sửa gì:

| Câu hỏi | Kết luận | Bằng chứng |
|---|---|---|
| Tiền xử lý còn gọi bge-m3 để embedding không? | **Còn.** Chỉ bảng `embed_cache` bị bỏ, không phải embedding | `embedding.provider=ollama`, `model=bge-m3:latest`; `articles.index_done_chunks` của bài 49/50/53 = 8355/7036/947 chunk (mỗi chunk 1 vector); log `ollama: gọi embed` + `ollama: embed xong` |
| "Tiền xử lý xong rồi" sao vẫn thấy gọi LLM? | Đó là **phase 3 `compile`** (sau index), không phải index gọi lại model | `cmd/worker/main.go` có schedule riêng `compile-articles` (30s); `article_sections` có 5–10 mục/bài ⇒ 5–10 call LLM/bài; `compile_attempts = 0`, `compile_status = done` cho 49/50/53 ⇒ mỗi bài chỉ compile 1 lần |

Bảng "phase nào gọi model nào" (hiện trạng, đúng theo code):

| Phase | Model | Khi nào |
|---|---|---|
| 0 extract | Tesseract OCR (VLM tắt: `vlm.enabled=false`) | mỗi trang scan/ảnh |
| 1 index | `embedding.model` = **bge-m3** (Ollama), 1 call/lô chunk + 1 call embed tên bài | mỗi đợt chunk |
| 2 compile | **LLM** `llms.default_provider` (local: `9router`, config.json: `ollama`) | **mỗi mục** tài liệu, cache Redis 30 ngày |
| chat | LLM (provider theo request) + bge-m3 cho câu hỏi | mỗi lượt hỏi |

Vấn đề thật: **log không nói call này của phase nào / model nào** — `compile` chỉ log
"xong 1 bài", `chat` không log gì khi gọi model, nên nhìn log (hoặc dashboard provider)
chỉ thấy "có call LLM" mà không biết ai gọi. Đợt này chỉ **làm rõ ranh giới call**, không
đổi kiến trúc pipeline.

## Phần A — Log đúng chỗ gọi model

- `article_compile_service.go` / `summarize`:
  - mỗi mục **cache miss** → `logs.Infow("compile: gọi LLM tóm tắt mục", article_id, name, section, chars, model)`
    — 1 dòng = 1 call thật, đếm được số call của 1 bài.
  - **cache hit** → `logs.Debugw("compile: cache hit, không gọi LLM", …)` (không nhiễu ở level info).
- `article_index_service.go` / `upsertBatch`: đổi `ollama: gọi embed` → `index: gọi embed`,
  `ollama: embed xong` → `index: embed xong` (giữ field `model`, cùng tiền tố stage như
  `extract:`/`compile:`).
- `agent/usecase/chat_service.go` / `Chat`: thêm `logs.Infow("chat: gọi agent", model, history, message_chars)`
  ngay sau `Resolve` — tách call của chat khỏi call của worker compile.

## Phần B — Dọn tài liệu sai

- `article_index_service.go` đầu `upsertBatch`: doc comment vẫn mô tả "hỏi `embed_cache`
  trước khi gọi model" — cache đã bỏ (dòng 55 nói rõ), giữ lại là tài liệu sai → viết lại
  thành "1 call `EmbedBatch`/đợt + 1 call embed tên bài, không có cache vector".
- `AGENTS.md`: thêm bảng "phase → model call" vào mục Ingestion pipeline để lần sau khỏi
  phải suy luận từ code.

## Kiểm chứng

- `go build ./...`, `go vet ./...`, `golangci-lint run ./...`, `go test ./...`.
- `gofmt -l` trên các file sửa (không mass-convert file CRLF sẵn có).
- `codegraph sync` sau khi sửa.

## Commit

1. `docs(plans): lên kế hoạch đợt 11`
2. `feat(observability): log rõ từng call embed và LLM theo phase`
3. `docs: bảng model theo phase trong AGENTS`
4. `docs(plans): ghi kết quả đợt 11`

## Ngoài phạm vi

- Không thêm cờ `compile.enabled` để tắt phase 3 (đổi hành vi + ảnh hưởng chat vì
  `article_sections` là nguồn tóm tắt).
- Không đổi model/provider mặc định.
