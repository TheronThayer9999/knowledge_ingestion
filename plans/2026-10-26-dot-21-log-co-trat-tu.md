# Đợt 21 — Log có trật tự: một chính sách, không rắc rối (field order + cắt giá trị + ẩn query link)

## Vấn đề

User báo log "khó nhìn đọc lắm": mỗi nơi viết một kiểu, giá trị dài tràn cả màn hình,
và có chỗ in cả link. Soi code thì đúng thật, nguồn gây nhiễu nằm ở 3 chỗ:

1. **Thứ tự field tùy hứng.** Cùng một request mà dòng này `trace_id` đứng đầu,
   dòng khác đứng cuối (`file_service.go` để `error` ở cuối, `chat_service.go` để
   `model` trước `trace_id`…), nên mắt không quét nhanh được, diff/grep hai dòng
   cùng kind cũng lệch.
2. **Giá trị không trần.** `SLOW SQL` in nguyên câu SQL; `chat_service.go:166,169`
   in **nguyên câu hỏi của user** vào log warn; `agent.go:178` in nguyên `query`
   (có thể là cả chỉ thị retry dài). Log warn để phục vụ chẩn đoán, không cần bản
   gốc nội dung.
3. **Chỗ in link / log vô cấu trúc.** `articles_repository.go:99` gọi
   `logs.Warn("error occurred while checking if url exists: ", url)` — tiếng Anh,
   printf-lẫn-positional, và nguyên văn URL của bài viết.

Hạ tầng LLM thì **đã ổn**: `loggingTransport` (đợt 18) chỉ ghi
`site/provider/model/host/path/status/elapsed_ms/trace_id`, không body, không URL
đầy đủ; `trace.go` cũng vậy. Nên đợt này không đụng hai chỗ đó.

## Thiết kế

**Chính sách đặt ở một chỗ duy nhất: `src/common/logs`.** Không sửa 100 call site
để sắp xếp tay — call site cứ giữ ý đồ của người viết, package logs chuẩn hoá
trước khi đưa xuống zap, nên mọi wrapper (kể cả logger gorm, transport LLM, access
log) đều tự động cùng khuôn.

`normalizeFields(kv []any) []any` áp cho mọi hàm dạng `…w`/`Error`:

| Bước | Quy tắc |
|---|---|
| Bóc cặp | kv thành từng cặp `key, value`; key không phải string hoặc số phần tử lẻ → **trả nguyên** (không đoán, giữ đúng hành vi zap) |
| Sắp tầng | ① `trace_id` → ② mọi key kết thúc `_id` (alphabetical) → ③ các key còn lại (alphabetical) → ④ `error` **luôn cuối** (giá trị dài và hay đổi nhất, đứng cuối thì dòng log đọc được tới chỗ cắt chung) |
| Cắt giá trị | value là `string`/`[]byte` dài quá `maxLogValueRunes = 512` → giữ 512 rune đầu + `…[+N ký tự]` (đếm **rune**, tiếng Việt không cắt giữa chữ); `error` để nguyên vì zap tự encode và message của provider đã được tóm ở `provider_error.go` |
| Ẩn query link | value có dạng URL tuyệt đối `http(s)://…?…` → rút gọn còn `scheme://host/path` + `[đã ẩn query]` (đây là chỗ `X-Amz-Signature`/`X-Amz-Expires` lọt vào log, và query của link người dùng là dữ liệu nhạy cảm hơn path) |

Sắp alphabetical trong từng tầng (không hard-code danh sách 30 key) để **không phải
sửa logs.go mỗi khi thêm field mới**, vẫn đủ trật tự để mắt quét và để hai dòng cùng
kind so sánh được.

**Sửa call site in nội dung** (phần chính sách không thay thế được):

| File | Việc |
|---|---|
| `src/agent/usecase/chat_service.go` | 2 dòng warn retry đổi `"message", dto.Message` → `"message_chars", len(dto.Message)` (dòng info đã dùng `message_chars` rồi — nhất quán) |
| `src/agent/eino/agent.go` | warn answer rỗng: `"query", query` → `"query_chars", len([]rune(query))` |
| `src/infrastructure/repository/articles_repository.go` | `logs.Warn(positional…)` → `logs.Warnw("article: đếm bài theo url lỗi", "trace_id", …, "user_id", userID, "error", err)`, **bỏ URL** (đã có trace_id + user_id là lần ra; nội dung link không phải thứ cần log). Vẫn để mức WARN như cũ vì logger gorm đã in "SQL error" kèm stack ở ERROR — thêm dòng ERROR nữa là ngập log |

Không đổi message tiếng Việt sẵn có, không đổi mức log — chỉ đổi hình thức.

## Tests

`src/common/logs/fields_test.go` (package `logs`, không cần logger thật — hàm thuần):
- đủ cặp + key string: ra đúng thứ tự `trace_id`, `*_id` (alphabetical), còn lại
  (alphabetical), `error` cuối;
- key không phải string / số lẻ → trả nguyên, không panic;
- cắt 512 rune + marker `…[+N ký tự]`, test rune-count với chuỗi tiếng Việt;
- URL có query → mất query, giữ host/path; URL không query → nguyên văn;
- `logs.Error(err, msg, kv)` đặt `error` cuối dù caller truyền kv gì.

`src/common/logs/logs_policy_test.go`: dựng logger với `zaptest/observer`, gọi
Infow/Warnw/Debugw/Error/InfowSkip/WarnwSkip rồi assert thứ tự field trong
`LoggedEntry.Context` — khoá rằng chính sách chạy thật trên đường đi xuống zap,
không chỉ trên hàm thuần (thêm test cắt giá trị dài và ẩn query qua wrapper).
Tên file là `logs_policy_test.go` vì `logs_test.go` **đã tồn tại** (khoá chính
sách stack-trace WARN/ERROR) — lần đầu sửa tưởng nó trống và ghi đè lên, đã
restore từ git rồi viết test ở file riêng.

## Kiểm chứng

- `go build ./...`, `go vet ./...`, `golangci-lint run ./...`, `go test ./...`
- `go test ./src/common/logs/ -v`
- Chạy thật `go run ./cmd/apis -configs ./configs/config.local.json`, gọi vài
  endpoint (login, list articles, presign, 1 bài nguồn URL để repo lỗi path), xem
  sample log: field cùng khuôn, không còn SQL/message nguyên văn tràn màn hình.
- `codegraph sync` sau khi sửa Go.

## Ngoài phạm vi (đợt sau)

- Chuẩn hoá số trích dẫn phía BE (Đợt 22) — "hạn chế để LLM đoán" là chuyện answer,
  không phải log.
- FE chat (Đợt 23), lịch sử chat DB (Đợt 24).

## Kết quả

### Đã làm

| File | Việc |
|---|---|
| `src/common/logs/fields.go` (mới) | `normalizeFields` + `fieldRank` (4 tầng), `sanitizeValue`, `truncateRunes` (trần 512 rune, marker `…[+N ký tự]`), `hideURLQuery` (URL tuyệt đối mất query/fragment + `[đã ẩn query]`) |
| `src/common/logs/logs.go` | Debugw/Infow/Warnw/Error + InfowSkip/WarnwSkip/ErrorSkip đều đi qua `normalizeFields`; hàm positional (Info/Warn/…f) giữ nguyên đường zap |
| `src/common/logs/fields_test.go` (mới) | 6 test thuần: thứ tự tầng, kv lẻ/key không phải chuỗi, cắt rune tiếng Việt, ẩn query (5 case), giữ nguyên error/số, không sửa slice caller |
| `src/common/logs/logs_policy_test.go` (mới) | 4 test qua `zaptest/observer`: 6 wrapper ra đúng thứ tự field, cắt giá trị dài thật trên đường xuống zap, ẩn `X-Amz-Signature`, log positional không bị đổi |
| `src/agent/usecase/chat_service.go` | 2 warn retry bỏ `"message", dto.Message` → `message_chars` + `trace_id`; dòng info và warn retry-lỗi cũng gắn `trace_id` (đếm rune thay byte) |
| `src/agent/eino/agent.go` | Tách `warnEmptyAnswer(ctx, query)`: log `trace_id` + `query_chars`, không in nguyên query |
| `src/infrastructure/repository/articles_repository.go` | `logs.Warn("error occurred…", url)` (Anh, positional, phơi URL) → `logs.Warnw("article: đếm bài theo url lỗi", trace_id, user_id, error)` — không còn URL |
| `AGENTS.md` | Thêm bullet convention về chính sách field log để code mới tự đi theo khuôn |

### Kiểm chứng

- `go build ./...`, `go vet ./...` sạch; `golangci-lint run ./...` → `0 issues.`;
  `go test ./...` → 20 packages `ok`, 0 FAIL (có `src/quality`).
- `go test ./src/common/logs/ -v`: toàn bộ test mới PASS, `TestStackOptionsOnlyFromError`
  cũ vẫn PASS (chính sách stack trace không đổi).
- `TestFunctionBudget` lần đầu BÁO ĐỎ: `(*ChatAgent).Run` lên 70 dòng trong khi
  allowlist ghi 67 — thay vì nâng số, tách `warnEmptyAnswer` ra hàm riêng (Run còn
  65 dòng). Allowlist giữ nguyên entry.
- `gofmt -l` trên bản chuyển LF của các file mới: không file nào bị báo (đúng lệ CRLF
  đã ghi ở Gotchas — không đổi line ending hàng loạt).

### E2E thật

Chạy `ki.exe` (build từ `cmd/apis`) với `configs/config.local.json` ở cổng 9001,
luồng: register `dot21user` (id 91) → login → category 127 → article 71 nguồn URL
→ download (400/404) → list → search → chat. 242 dòng log, không dòng nào vượt
331 ký tự:

1. Khuôn mới thấy rõ ngay cả với log phát ra từ thư viện (gorm logger):
   `SQL record not found {"trace_id": "…", "elapsed": "3.9311ms", "rows": 0, "sql": "SELECT …"}`
   và access log `HTTP request {"trace_id": "…", "latency": …, "method": …, "path": …, "status": …}`.
2. `article: tạo bài, chờ worker {"trace_id": "…", "article_id": 71, "category_id": 127,
   "user_id": 91, "content_type": "text/html", "doc_type": "general", "file":
   "https://example.com/quy-che [đã ẩn query]", "name": "bai nguon url"}` — URL gửi vào
   dài ~700 ký tự và chứa `token=secret123`; log chỉ còn scheme://host/path. Query
   vẫn nguyên trong DB (kiểm tra bằng psql) nên chức năng không đổi.
3. `chat: gọi agent {"trace_id": "…", "history": 0, "message_chars": 20, "model": "…"}`
   — không còn nguyên văn câu hỏi của user trong log retry; 5 cặp `llm: request`/
   `llm: request xong` của một lượt chat đều cùng `trace_id` nên đếm được bao nhiêu
   vòng tool.
4. Quét cả file log: `X-Amz-Signature` = 0, `secret123` = 0.
5. Trần cắt 512 rune **không bị kích hoạt trong luồng này** (dòng log dài nhất chỉ
   331 ký tự, nên chưa giá trị nào chạm trần), phần đó chỉ có bằng chứng từ unit
   test.

### Dọn dẹp

- Soft-delete article 71, category 127, user 91 bằng `docker exec -i theron_postgres
  psql` — luồng test sinh mật khẩu ngẫu nhiên và không lưu lại nên không đăng nhập
  lại để xoá qua API được; bài 71 chưa extract (worker tắt) nên không có vector sót.
- Đã kill `ki.exe` (PID 12236), xoá `ki.exe`, `dot21_flow.ps1` và log tạm trong `%TEMP%`.

### Sai sót gặp phải trong lúc làm

- `src/common/logs/logs_test.go` **đã tồn tại** (test khoá stack trace) nhưng summary
  cũ ghi là package logs chỉ có 1 file, nên tôi ghi đè mất nó; phát hiện ngay sau đó
  và `git checkout` restore, viết test mới ở `logs_policy_test.go`. Bài học: kiểm tra
  file có trước khi Write đè.
- Sửa comment tiếng Việt thiếu dấu ở lần viết đầu (`fields.go` bị ASCII hoá) — viết
  lại toàn bộ comment có dấu để khớp convention.
