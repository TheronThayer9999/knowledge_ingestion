# Đợt 17 — Bỏ key thật khỏi `configs/config.json` (file được track)

## Vấn đề

`configs/config.json` đang được git track và chứa **API key thật của Ollama Cloud**
trong `llms.providers.ollama.api_key`. Key này đã vào lịch sử git từ commit `5a59c07`
(`feat(agent): add list_categories tool, ...`) nên phải coi như **đã lộ** — xoá khỏi
file hiện tại chỉ chặn việc lộ tiếp, không làm key cũ an toàn trở lại.

## Thay đổi

| File | Việc |
|---|---|
| `configs/config.json` | `llms.providers.ollama.api_key` → `""` (các provider khác vốn đã rỗng) |
| `README.md` | Thêm gạch đầu dòng cạnh mục Config: key lấy từ env `<TÊN_PROVIDER>_API_KEY` (vd `OLLAMA_API_KEY`, `GROQ_API_KEY`) hoặc từ `configs/config.local.json` (đã gitignore) — không commit secret vào `config.json` |

## Vì sao để rỗng là an toàn với code hiện tại

`config.Load` đã hỗ trợ `os.Getenv(strings.ToUpper(name) + "_API_KEY")` ghi đè `api_key`
(src/config/config.go:344), nên không cần sửa code để cấp key lại. App vẫn boot bình
thường: provider type `openai` thiếu key thì bị bỏ qua kèm warn
`agent: bỏ qua provider (thiếu cấu hình)`; provider type `ollama` vẫn nạp (local Ollama
vốn không cần key) nhưng không còn header Authorization — nên request tới
`https://ollama.com` sẽ 401 cho tới khi cấp `OLLAMA_API_KEY`.

## Không làm (cần user quyết)

- **Không rotate key**: chỉ chủ tài khoản Ollama làm được (revoke key cũ + tạo key mới
  trên ollama.com). Đây là việc BẮT BUỘC vì key đã nằm trong lịch sử git.
- **Không viết lại lịch sử git** (`git filter-repo`/BFG) — thao tác phá huỷ, phải có
  yêu cầu rõ ràng của user.
- Không đổi `llms.default_provider` (vẫn `ollama`); máy dev dùng
  `configs/config.local.json` với default `9router` nên không bị ảnh hưởng.

## Kiểm chứng

1. `go build ./... && go vet ./... && golangci-lint run ./...`, `go test ./...`.
2. Chạy `go run ./cmd/apis -configs ./configs/config.json`: app phải vẫn lên (provider
   `ollama` nạp được nhưng không có Authorization — chỉ 401 khi thực sự gọi).
3. `git grep` key cũ trong working tree: không còn kết quả.

## Kết quả

### Đã làm

- `configs/config.json`: `llms.providers.ollama.api_key` từ key thật → `""`. Không đụng
  provider nào khác, không đổi `default_provider`.
- `README.md`: thêm gạch đầu dòng ngay dưới mục Config — secret không nằm trong file
  track, cấp qua `<PROVIDER>_API_KEY` hoặc `configs/config.local.json`; provider thiếu
  key bị bỏ qua lúc boot (app vẫn lên).

### Kiểm chứng

| Việc | Kết quả |
|---|---|
| `go build ./...` / `go vet ./...` / `golangci-lint run ./...` | OK / OK / `0 issues.` |
| `go test ./...` | tất cả package pass (gồm `src/config`, `src/quality`) |
| `git grep BCuo3MJrpLB5Wv7WkssXu2Yc` | không còn kết quả trong working tree |
| Chạy `dot17_apis.exe -configs ./configs/config.json` | `llm selected {ollama, gemma4:31b}` → warn bỏ qua `openrouter`, `groq`, `9router`, `nvidia` (`api_key is required`) → `application started {config: ./configs/config.json}` |

### Hệ quả cần biết

- Provider `ollama` (type `ollama`) **vẫn được nạp** vì local Ollama không cần key —
  chỉ là không còn header Authorization, nên gọi `https://ollama.com` sẽ 401. Muốn
  dùng lại Ollama Cloud: `$env:OLLAMA_API_KEY="<key mới>"` rồi chạy lại (env đè file).
- Key cũ **vẫn nằm trong lịch sử git** (commit `5a59c07`) ⇒ **phải revoke + tạo key mới**
  trên ollama.com. Mình không rotate được và không tự viết lại lịch sử git.
- Máy dev đang dùng `configs/config.local.json` (gitignored, default `9router`) nên
  không bị ảnh hưởng.
