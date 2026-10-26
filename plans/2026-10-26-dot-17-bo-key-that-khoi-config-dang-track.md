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

`config.Load` đã hỗ trợ `os.Getenv(strings.ToUpper(name) + "_API_KEY")` ghi đè
`api_key` (src/config/config.go:344), và `BuildProviderAgents` bỏ qua provider thiếu
key kèm warn `agent: bỏ qua provider (thiếu cấu hình)` — app vẫn boot, chỉ provider
`ollama` không dùng được cho tới khi cấp key qua env.

## Không làm (cần user quyết)

- **Không rotate key**: chỉ chủ tài khoản Ollama làm được (revoke key cũ + tạo key mới
  trên ollama.com). Đây là việc BẮT BUỘC vì key đã nằm trong lịch sử git.
- **Không viết lại lịch sử git** (`git filter-repo`/BFG) — thao tác phá huỷ, phải có
  yêu cầu rõ ràng của user.
- Không đổi `llms.default_provider` (vẫn `ollama`); máy dev dùng
  `configs/config.local.json` với default `9router` nên không bị ảnh hưởng.

## Kiểm chứng

1. `go build ./... && go vet ./... && golangci-lint run ./...`, `go test ./...`.
2. Chạy `go run ./cmd/apis -configs ./configs/config.json`: app phải vẫn lên, log có
   warn bỏ qua provider `ollama`, các provider khác vẫn nạp.
3. `git grep` key cũ trong working tree: không còn kết quả.
