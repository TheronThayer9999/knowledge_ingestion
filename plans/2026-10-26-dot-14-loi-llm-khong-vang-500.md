# Đợt 14 — Lỗi LLM / client ngắt không còn văng thành "lỗi runtime 500"

## Bối cảnh

Log thật đợt 12 (provider `9router`, `POST /api/v1/chat`):

```
INFO  usecase/chat_service.go:151  chat: gọi agent {"model": "cl/nvidia/nemotron-3.5-super-vl:free", ...}
ERROR apis/base.go:91              unhandled error {"error": "eino agent: [NodeRunError] failed to create chat completion:
                                   Post \"http://localhost:20128/v1/chat/completions\": context canceled\n---\nnode path: [node_1, ChatModel]"}
knowledge_ingestion/src/controller/apis.Render[...]
        D:/code/projects/taskforge/knowledge_ingestion/src/controller/apis/base.go:91
```

Hai chỗ sai:

1. Nguyên nhân là **client bỏ ngang** (apiprobe hết `timeout_ms` 120s) hoặc **provider chậm quá
   `timeout_sec`** — không phải bug trong code Go. Nhưng `Render` coi là lỗi "unhandled" ⇒ log
   **ERROR kèm stack 20 dòng**, đọc như panic/lỗi logic, che mất lỗi thật trong log.
2. Response là **500 "internal server error"** — người gọi không biết là "hết giờ chờ" hay "đã bị
   hủy", còn hiểu là server hỏng.

Nguyên nhân code: `providerError` đã map 400/404/422 (model sai), 429 (quota), 502 (5xx provider),
503 (không kết nối được hạ tầng), nhưng **cố ý không map `context.Canceled`** (để `dependencyError`
khỏi gán 503 sai — xem comment trong file) nên lỗi rơi xuống nhánh cuối "trả nguyên" ⇒ `Render`
map 500 + stack. `context.DeadlineExceeded` (do `http.Client.Timeout` của provider) cũng không có
nhánh nào.

Chuỗi lỗi thật bị eino bọc thành `[NodeRunError]` nên `Unwrap` đứt — không bắt bằng `errors.Is`
được, phải đọc text như `depDownMarkers` đang làm.

## Mục tiêu

1. `context canceled` (client ngắt: nút Dừng, đóng tab, curl `--max-time`) → **499** kèm message
   "client đã ngắt yêu cầu", log **WARN 1 dòng**, không stack.
2. `context deadline exceeded` (provider không trả lời trong `timeout_sec`) → **504** kèm message
   "hết thời gian chờ…", log **WARN 1 dòng**, không stack.
3. Lỗi thật (parse, SQL, bug logic) **giữ nguyên** ERROR + stack + 500 — không được nuốt thành 499/504.
4. Message trả client không lộ URL/cổng/`dial tcp` của hạ tầng nội bộ.

## Thay đổi

### `src/common/errors/errors.go`

- Thêm hằng `ClientClosed = 499` (quy ước nginx, `net/http` không có).
- Thêm `ContextError(err error) *Error`:
  - `errors.Is(err, context.Canceled)` **hoặc** text `context canceled` → 499
    "client đã ngắt yêu cầu trước khi xử lý xong".
  - `errors.Is(err, context.DeadlineExceeded)` **hoặc** text `context deadline exceeded` → 504
    "hết thời gian chờ: dịch vụ phía sau không phản hồi kịp — thử lại hoặc thu hẹp phạm vi yêu cầu".
  - Lỗi khác → `nil` (caller giữ đường 500 + stack).
- Kiểm tra text là bắt buộc vì `[NodeRunError]` của eino cắt `Unwrap`.

### `src/agent/usecase/provider_error.go`

- Đầu `providerError`: `if ce := errors.ContextError(err); ce != nil { logs.Warnw(...); return ce }`.
  Đặt **trước** `eino.AsProviderHTTPError` vì khi ctx đã chết thì status provider (nếu có) không còn
  nghĩa. Log kèm `model` + `status` để một dòng là đủ chẩn đoán (Render thấy `*errors.Error` hợp lệ
  nên không log nữa — cùng lý do `dependencyError` tự log).
- Cập nhật comment đầu hàm cho khớp.

### `src/controller/apis/base.go`

- `Render`: trước `logs.Error`, thử `errors.ContextError(res.Err)`, nếu nil thì
  `errors.ContextError(c.Request.Context().Err())` → log WARN + `ErrorData(ce)`. Đây là lưới an
  toàn cho mọi endpoint (search, upload…), kể cả khi service trả lỗi raw trong lúc ctx đã hủy
  ("agent hết vòng lặp"). Chỉ nhánh còn lại mới ERROR + stack + `ErrInternal`.

## Test

Unit:

- `src/common/errors/context_error_test.go`: bảng — wrapped `context.Canceled`,
  text kiểu eino (không Unwrap được), wrapped `context.DeadlineExceeded`, text
  `context deadline exceeded (Client.Timeout exceeded while awaiting headers)`, lỗi lạ → nil; và
  message không chứa `localhost`/`:20128`/`dial tcp`.
- `provider_error_test.go`: cancel → 499; deadline text → 504 (giữ test cũ: lỗi lạ vẫn trả nguyên).
- `base_test.go`: `Render` với request ctx đã hủy + lỗi raw → status 499, envelope đủ 4 field.

Thật (API ở 9001 với `configs/config.local.json`, provider `9router`):

- a) `curl.exe --max-time 2` vào `/api/v1/chat` (client bỏ ngang) → access log `status=499`, dòng
  WARN `chat: dừng vì …` **không stack**, không còn `ERROR unhandled error`.
- b) Provider treo: TCP stub `127.0.0.1:20999` accept mà không trả lời + config tạm trong `$env:TEMP`
  (`timeout_sec: 5`, app port 9002) → chat trả **504** sau ~5s với message đọc được.
- c) Hồi quy: sai model trên 9router vẫn **400**, chat hợp lệ vẫn **200**.

Verify: `go build ./... && go vet ./... && golangci-lint run ./...` + `go test ./...`; `codegraph sync`
sau khi sửa Go.

## Rủi ro / ghi chú

- 499 không phải mã chuẩn RFC; nó chỉ để access log phân biệt "client bỏ" với "server hỏng". Client
  đã ngắt thì body 499 gần như không tới tay ai — mục đích chính là log/hành vi đúng.
- Nếu ctx hủy đúng lúc service trả lỗi thật thì ưu tiên 499: client đã đi rồi, code 500 vô nghĩa.
- Không đổi `dependencyError` (503 "chưa bật Ollama/Qdrant" vẫn đúng nghĩa hạ tầng chết).

## Kết quả

### Đã sửa

| File | Việc |
|---|---|
| `src/common/errors/errors.go` | `ClientClosed = 499` + `ContextError(err)` map cancel→499, deadline→504 (bắt cả `errors.Is` lẫn text kiểu eino) |
| `src/agent/usecase/provider_error.go` | `providerError` xét ctx **trước** `AsProviderHTTPError`, log WARN 1 dòng (`status`, `model`) không stack |
| `src/controller/apis/base.go` | `Render` gọi `requestContextError` (lỗi service + ctx request) trước nhánh `logs.Error` thăm dò 500 |

### Test thật (API `-configs configs/config.local.json`, provider `9router`)

| # | Case | Kỳ vọng | Kết quả |
|---|---|---|---|
| a | `curl.exe --max-time 2` chat 9router (client bỏ ngang) | 499 + WARN không stack | ✅ `status=499`, latency 2.008s, **không** có dòng `ERROR unhandled error` |
| b | Provider treo (TCP stub `:20999` accept nhưng không trả lời, `timeout_sec: 5`, app 9002) | 504 sau ~5s | ✅ `http=504` @5.05s, body `hết thời gian chờ: dịch vụ phía sau không phản hồi kịp…` |
| c | Chat sai model (`meta/khong-ton-tai`) | vẫn 400 | ✅ `code=400` "provider từ chối model … No active credentials for provider: meta" @26ms |
| c | Chat hợp lệ (`Xin chào`) | vẫn 200 | ✅ 200 @2.35s, `question_type=chitchat`, answer 300 ký tự |

Log sau khi sửa (case a — trước đây là `ERROR … unhandled error` + 20 dòng stack):

```
12:17:08 INFO  usecase/chat_service.go:151   chat: gọi agent {"model": "kc/nvidia/nemotron-3.5-super-vl:free", …}
12:17:10 WARN  usecase/provider_error.go:75  chat: dừng giữa đường (client ngắt hoặc provider quá hạn) {"status": 499, "model": "kc/nvidia/…"}
12:17:10 INFO  middlewares/trace.go:63       HTTP request {"path": "/api/v1/chat", "status": 499, "latency": "2.0082453s"}
```

Case b (provider treo) tương tự nhưng `status=504`, `latency=5.0065895s` — `WARN` vẫn không kèm stack.

### Unit test thêm mới

- `src/common/errors/context_error_test.go`: 6 case (cancel/deadline ở cả dạng còn `Unwrap` lẫn chỉ còn text; lỗi thật → nil) + khoá message không lộ `localhost`/`20128`/`dial tcp`/`NodeRunError`.
- `TestProviderErrorMapsClientCancelTo499`, `TestProviderErrorMapsProviderTimeoutTo504` trong `provider_error_test.go`.
- `TestRenderMapsDeadContextTo499` trong `base_test.go` (envelope đủ 4 field, `message` nói rõ client đã ngắt).

Verify: `go build ./...` ✅, `go vet ./...` ✅, `golangci-lint run ./...` **0 issues** ✅, `go test ./...` **20/20 package ok** (gồm `src/quality` guardrail function budget).

### Đánh giá

1. **Đúng nguyên nhân người dùng chỉ ra**: LLM lỗi (hết giờ/bị ngắt) không còn văng ra như "lỗi runtime" — log 1 dòng WARN không stack, HTTP 499/504 thay vì 500.
2. **Lỗi thật vẫn giữ đường cũ**: `unhandled error` + stack + 500, nên log ERROR lại có giá trị chẩn đoán (đợt 10 thêm stack từ ERROR là để dùng cho đúng các ca này).
3. **`Render` là lưới an toàn toàn cục**: cả `/search`, upload, endpoint khác cũng hưởng, không chỉ chat.
4. **Không cần sửa FE**: FE hiển thị `message` từ envelope nên sẽ hiện "hết thời gian chờ: dịch vụ phía sau không phản hồi kịp…" thay cho "internal server error".
5. **Còn lại (ngoài phạm vi đợt này)**: `/api/v1/chat` không có timeout phía server — server chỉ dừng khi client bỏ hoặc `timeout_sec` của provider hết. Nếu muốn chặn agent loop 12 vòng treo lâu, cần thêm `context.WithTimeout` ở handler/service (đợt riêng, ảnh hưởng cả `label`/retry).

### Trạng thái sau test

- API 9001 chạy **code mới** với `configs/config.local.json` (phục vụ FE).
- Đã dừng và xoá: API test ở 9002, TCP stub 20999, config tạm `$env:TEMP\ki_stub.json`, các file body/response tạm.
- `git status` sạch sau commit code + kết quả.
