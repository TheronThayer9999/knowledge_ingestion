# Đợt 2 — VLM reader theo OOP/SOLID

## Mục tiêu

Có adapter đọc ảnh trang tài liệu bằng model ảnh (qwen-vl), đặt sau **hai interface hẹp**
theo đúng khuôn DIP của repo (`domain.Embedder` + `fx.Provide`/`fx.Invoke`), chưa nối vào
pipeline nào. Chưa có consumer ⇒ không đổi hành vi hiện tại.

## Nguyên lý áp dụng (ánh xạ cụ thể, không nói suông)

| Nguyên lý | Hiện thực |
|---|---|
| **SRP** | `domain.ImageRef` chỉ validate ảnh; `ollama.Reader` chỉ HTTP + parse; `ollama.Prompts` chỉ giữ text prompt; nội dung prompt nằm ở `prompts/*.md` |
| **OCP** | `Ask` và `Transcribe` là **hai method**, không phải `Read(mode string)`. Thêm hành vi mới (`ExtractTable`) = thêm method, không sửa method cũ |
| **LSP** | Mọi implementation phải chịu được cùng bộ test: rỗng → lỗi sentinel, 4xx → `IsPermanentError=true` |
| **ISP** | 2 vai: `IVLMReader` (pipeline extract) và `IVLMAsker` (tool của agent). 1 struct implement cả hai, fx bind từng vai — giống `domain.ArticleExtractQueue` ở `loader/worker.go:104` |
| **DIP** | Consumer nhận interface; `ollama.Config` là struct riêng (như `extractor.OCRConfig`) để infra **không import** package `config` |

## File

| File | Việc |
|---|---|
| `src/domain/vlm_reader.go` | `ImageRef` (value object), `Reading`, `IVLMReader`, `IVLMAsker`, sentinel lỗi |
| `src/infrastructure/vlm/ollama/client.go` | `Config`, `Prompts`, `Reader`, `NewReader` |
| `src/infrastructure/vlm/ollama/client_test.go` | `httptest.Server` giả lập Ollama; không cần model thật |
| `src/config/config.go` | `VLMConfig` + `GetVLM()` + default trong `Load` |
| `configs/config.json` | section `vlm` (mặc định `enabled: false`) |
| `prompts/vlm_transcribe.md`, `prompts/vlm_ask.md` | 2 prompt, load lúc startup, thiếu file ⇒ fail-fast |
| `src/loader/vlm.go` | `vlmOptions()` — provide Config/Prompts/Reader + bind 2 vai |
| `src/loader/loader.go`, `src/loader/worker.go` | gọi `vlmOptions()` (API và worker đều cần: extract dùng `Transcribe`, agent dùng `Ask`) |

## Quyết định

- **Không dùng `format: "json"`** cho `Transcribe` (khác §6.12 lúc phác): `Transcribe` trả
  markdown cả trang, ép JSON sẽ phải escape toàn bộ xuống dòng thành `\n` — vừa tốn token
  vừa dễ mất cấu trúc bảng. Chỉ `Ask` mới cần câu trả lời ngắn, và câu trả lời ngắn thì
  không cần JSON để parse.
- Trần ảnh nằm ở `Config.MaxImageBytes` (mặc định 10MB) do **Reader** kiểm, không phải
  `ImageRef` — value object không được biết cấu hình vận hành.
- Không có `fx.Invoke` cho VLM: chưa consumer nào dùng nên provider giữ lazy, app boot y
  như cũ kể cả khi Ollama không chạy.
- `IsPermanentError` coi 4xx là vĩnh viễn, **trừ 408 và 429** (timeout/rate limit thì retry
  được) — cùng ngữ nghĩa với `domain.Embedder`.

## Nghiệm thu

- [x] `go build ./...`, `go vet ./...`, `golangci-lint run ./...`, `go test ./...` xanh.
- [x] Test `httptest` khoá: payload gửi đi có `model`, `stream:false`, `images` base64; 2xx
      parse đúng `message.content`; 400 ⇒ permanent; 429/503 ⇒ không permanent; content
      rỗng ⇒ lỗi; ảnh quá trần ⇒ `ErrImageTooLarge`; `NewImageRef` từ chối rỗng/MIME lạ.
- [x] Boot API/worker không đổi hành vi (không thêm network call, không `fx.Invoke`).

## Kết quả

- `golangci-lint run ./...` → 0 issues; `go test ./...` toàn repo xanh.
- Boot thật `./cmd/apis`: `[Fx] RUNNING`, có `domain.IVLMReader` + `domain.IVLMAsker`
  trong graph nhưng **không dòng `[Fx] RUN provide ... vlmOptions`** nào — xác nhận
  provider lazy đúng như thiết kế (Ollama tắt vẫn boot được).
- Thêm `GetVLM()` vào `IConfig` làm gãy 2 fake config trong test cũ
  (`postgres/connection_test.go`, `seaweedfs/storage_test.go`) — đã bổ sung, đây là cái giá
  của interface rộng; nếu sau này phải sửa nhiều fake nữa thì nên tách `IConfig` thành
  các interface nhỏ theo nhóm (app/db/llm).
- Sự cố khi tạo file: `Write` sinh trùng dòng `package` ở 2 file ⇒ lỗi biên dịch ngay; bài
  học: sau mỗi lần tạo file mới phải chạy `gofmt -l` + `go build` trước khi tin là xong.

## Commit

1. `docs(rag): add batch 2 plan for VLM reader`
2. `feat(vlm): add ollama qwen-vl reader behind narrow domain interfaces`
3. `chore(config): add vlm section and wire it in loaders`
