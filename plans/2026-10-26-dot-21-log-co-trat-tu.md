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
| `src/infrastructure/repository/articles_repository.go` | `logs.Warn(positional…)` → `logs.Error(err, "article: đếm bài theo url lỗi", "trace_id", …, "user_id", userID)`, **bỏ URL** (đã có trace_id + user_id là lần ra; nội dung link không phải thứ cần log) |

Không đổi message tiếng Việt sẵn có, không đổi mức log — chỉ đổi hình thức.

## Tests

`src/common/logs/fields_test.go` (package `logs`, không cần logger thật — hàm thuần):
- đủ cặp + key string: ra đúng thứ tự `trace_id`, `*_id` (alphabetical), còn lại
  (alphabetical), `error` cuối;
- key không phải string / số lẻ → trả nguyên, không panic;
- cắt 512 rune + marker `…[+N ký tự]`, test rune-count với chuỗi tiếng Việt;
- URL có query → mất query, giữ host/path; URL không query → nguyên văn;
- `logs.Error(err, msg, kv)` đặt `error` cuối dù caller truyền kv gì.

`src/common/logs/logs_test.go`: dựng logger với `zaptest/observer`, gọi `Infow`/
`Error` rồi assert thứ tự field trong `LoggedEntry.Context` — khoá rằng chính sách
chạy thật trên đường đi xuống zap, không chỉ trên hàm thuần.

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
