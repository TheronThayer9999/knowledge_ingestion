# AGENTS.md

## Memory & MCP

- 4 MCP project (`.opencode/opencode.json`): `postgres` (read-only — ghi DB qua `docker exec -i theron_postgres psql -U theron_user -d manager_db`), `qdrant` (read-only, collection `article_chunks`), `codegraph` (tra cứu symbol/call-path — sửa code xong chạy `codegraph sync`), `apiprobe` (test API: `http_request`/`api_test`/`openapi_endpoints` + `save_auth`/`list_auth`/`delete_auth`; binary build từ source ở `D:\code\tools\api_tester_mcp` vào `GOPATH/bin`)
- Đầu việc chạy `/recall <từ khóa>` (quên thì đọc dòng này) — agentmemory giữ quyết định/bug/convention các session trước; xong việc đáng nhớ thì `/remember <nội dung>`

## Commands

- Verify: `go build ./...` && `go vet ./...` && `golangci-lint run ./...` (default linters, CỐ Ý không có `.golangci.yml` — đừng tạo lại); tests: `go test ./...` (`seaweedfs`, `postgres`, `apis`); CI: `.github/workflows/ci.yml` chạy lint job (golangci-lint-action@v9) + build/test job song song trên push/PR
- Run: `go run ./cmd/apis` → `GET /api/v1/ping`, `GET /api/v1/pong`, Swagger UI at `/swagger/index.html`; worker: `go run ./cmd/worker`
- FE: `cd knowledge_ingestion_fe` rồi `npm run lint` (`ng lint`, ESLint 9 flat config `eslint.config.js` — angular-eslint + typescript-eslint + template a11y, Prettier tắt rule format qua `eslint-config-prettier`) và `npm test` (vitest) / `npm run build`; `npm run lint` KHÔNG tự format — format bằng `npx prettier --write`
- Config: `-configs <path>` flag, default `./configs/config.json`; `APP_CONFIG_PATH` env overrides the flag default
- Prompts: system prompt nằm ở `prompts/` (`chat_system.md` tổng hợp, `classify_system.md` vòng 1 phân loại), load lúc startup (đường dẫn qua `agent.prompt_dir` trong config, default `./prompts`) — sửa prompt rồi restart app là ăn, không cần build lại; file thiếu thì app fail-fast (không còn bản mặc định trong code)
- Swagger: regenerate after changing annotations with `swag init -g cmd/apis/main.go` (swag CLI in `%USERPROFILE%\go\bin`); `docs/` is generated — never hand-edit it
- `go mod tidy` after adding/removing imports (it aggressively drops unused deps — it removed gorm/pgx once when nothing imported them)

## Architecture

- Go 1.26.5, single module `knowledge_ingestion`; import paths are `knowledge_ingestion/src/...` — keep this prefix, all code lives under `src/`
- Entrypoint `cmd/apis/main.go`: `fx.New(Provide(config), Options(loader.Load()...), Invoke(serverLifecycle))` then manual `Start` → OS signal → `Stop` (not `fx.Run`); `cmd/worker/main.go` dùng `loader.LoadWorkerInfra()` — cùng khung main
- All DI wiring lives in `src/loader/` (`loader.go` cho API/infra, `agent.go` cho wiring agent — tool/prompt/model mới của agent sửa ở đây, `worker.go` `LoadWorkerInfra()` cho worker) — `Load()` grouped as loadAdapter/loadService/loadValidator/loadEngine (same pattern as the shoe_shop fxloader); add new providers there only, never as package-level `var Module`
- Layer flow: `routers` (route table only, exposes `Engine`; HTTP server lifecycle is in `serverLifecycle` in cmd/apis/main.go) → `apis` (gin handlers) → `services` (business logic behind `I…` interfaces) → `domain` models + `controller/dtos`; config injected everywhere via `config.IConfig`
- Storage: contract `common/storage.IStorage` (`Save`/`Open`/`Delete`), implemented by `infrastructure/seaweedfs` with aws-sdk-go-v2 against the S3-compatible gateway (path-style, static creds, region fixed `us-east-1`); provided in `loadAdapter` — fx builds it lazily, so the app boots without SeaweedFS running
- DB: `infrastructure/postgres.NewConnection` (wired in `loadAdapter`) uses gorm (`gorm.io/gorm` + `gorm.io/driver/postgres`) and returns `postgres.IDB` (`GetDB() *gorm.DB`); pool tuned 25 open/10 idle/5m lifetime, ping fail-fast 10s — but `loadAdapter` có `fx.Invoke(func(postgres.IDB){})` force kết nối + AutoMigrate lúc startup, nên app **fail-fast** nếu DB không reachable (trước đây fx lazy nên boot được khi Docker tắt); `connection_test.go` dials the config host, falls back to `127.0.0.1`, skips when unreachable
- Former scaffold dirs (`common/utils`, `controller/middlewares`, `infrastructure/repository`) are now populated — follow the existing layer pattern when adding files

### Ingestion pipeline (worker, 2 phase — both cùng chạy trong `cmd/worker`)

| Phase | Cột queue | Service | Việc |
|---|---|---|---|
| **0 extract** | `extract_status/attempts/next_retry_at`, `extracted_at` | `services.article_extract_service.go` | tải blob SeaweedFS → OCR (nếu PDF scan) → ghi **TEXT THÔ từng trang** vào bảng `article_pages` |
| **1 index** | `index_status/attempts/next_retry_at`, `index_done_chunks`, `indexed_at` | `services.article_index_service.go` | đọc `article_pages` theo cửa sổ trượt → `SplitBlocks` → cắt tại ranh giới trang → embed → upsert Qdrant + checkpoint |

- **KHÔNG có cột text toàn file** (`articles.extracted_text` đã bị drop ở `migrateQueueColumns) — nội dung nằm ở `article_pages` theo từng trang. Lý do: phase index đọc `ListPagesRange(articleID, fromPage, INDEX_PAGE_WINDOW)` nên **RAM là hằng số, không phụ thuộc kích thước file** (file 500 trang không kéo hết lên RAM như cũ). `article_pages` được **giữ lại** sau extract (chỉ `purge` mới xoá) nên đổi config chunker không tốn OCR lần nào.
- 2 việc phụ thuộc **CẢ file** (bỏ dòng lặp header/footer, khử chữ rải) chạy bằng `extractor.Cleaner`: pass 1 `Scan` chỉ **cộng số** (không giữ text), pass 2 `Apply` áp từng trang — cùng kết quả nhưng RAM O(1 trang). Vì vậy `article_pages` lưu **thô**, không sạch sẵn.
- Checkpoint index = `articles.index_done_chunks` (1 số nguyên, không phải bảng row). Chunk sinh lại **tất định** từ cùng text + cùng config, PointID là UUIDv5 (`utils.NewPointID(articleID, chunkIndex)`) nên làm lại đợt đó ghi đè đúng point cũ, không nhân bản. `resumeFrom == 0` thì `DeleteByArticle` trước khi ghi (dọn đuôi thừa khi đổi chunker).
- Cửa sổ luôn kết thúc ở **ranh giới trang** (`pageWindow.cuts`) → `PageNum` chính xác tuyệt đối cho trích dẫn, chunk không tràn 2 trang. `SplitBlocks` chạy TRONG cửa sổ nên vẫn thấy tiêu đề Điều ở tầm vài trang.
- Điều kiện dừng vòng đọc là **`len(pages) == 0`**, TUYỆT ĐỐI không dùng "lượt đọc ngắn hơn window" — repo khác/`INDEX_WINDOW_MAX_CHARS` cắt cửa sổ sớm vẫn trả đủ trang, coi lượt ngắn là hết file là âm thầm bỏ sót nửa tài liệu (test `TestIndexOneReadsInWindows` khoá đúng cái này).

## Conventions

- Interfaces prefixed `I` (IConfig, IPingPongService, IDB); constructors take interfaces, fx binds interface → concrete (SOLID/DIP)
- Services never return bare values/errors — they return `dtos.Result[T]` built with `dtos.Ok(data)` / `dtos.Fail(err)`; handlers map it in one call: `Render(c, a, res)` (Render takes the `renderer` interface — any controller embedding `*baseController` satisfies it, so no need to name the struct). Render maps `*errors.Error` → its own HttpCode/Code; any other error → `logs.Error` + `ErrInternal` (500), never a bare 400 and never leaking internals — so services wrap client-visible errors with `NewCustomHttpError` and let everything else stay plain
- `domain/models` are gorm persistence models: embed `BaseModel` by value (ID/DeletedAt/CreatedAt/UpdatedAt); `DeletedAt` is `gorm.DeletedAt` (soft delete) tagged `json:"-"` (gorm.DeletedAt has no custom marshaller, JSON would be `{Time,Valid}`), secrets like `Password` are `json:"-"` too
- DTOs live in `src/controller/dtos` (package `dtos`, full guideline là comment block đầu `user_dto.go`): naming `<Action><Entity>Request` / `<Entity>Response`, NO `Dto` suffix (`CreateUserRequest`, `UserResponse`), Go initialism (`Username` not `UserName`, `ID`); một kiểu JSON (`snake_case`) cho cả project; request và response luôn là struct riêng — request field bắt buộc có tag `json` tường minh (cấm `json:"-"`, `ShouldBindJSON` sẽ bỏ qua), validation đặt trong `binding` (`required,email,min,max`; optional = `omitempty,rule`; partial update = field pointer `*string` để phân biệt "không gửi" vs "rỗng"); response KHÔNG khai báo field nhạy cảm (password/token) — đừng che bằng `json:"-"`, hãy không khai báo; mapper viết tay (`ToUserResponse`, `ToModel`) cùng package — **service nhận DTO, gọi mapper để convert sang `domain` model và map sang response trước khi trả về; repository chỉ làm việc với `domain` model**; service hash password (bcrypt/argon2) trước khi giao repo lưu; controller chỉ validate (`Bind`) + `Render`, không map gì thêm
- Handlers embed `*baseController` (`apis/base.go`): `Success`/`ErrorData`/`BadRequest` emit the `{status, code, message, data}` envelope (`dtos.ResponseResource`), `Bind` = bind + validate — never return raw JSON from a handler
- Custom errors live in `src/common/errors`: `errors.Error` carries `Code` (body) + `HttpCode` (status line); build with `NewCustomHttpError`, unwrap with `errors.From`, sentinels `Success/BadRequest/Internal` + `ErrBadRequest/ErrInternal` — the package name shadows stdlib `errors`, so alias stdlib when a file needs both
- Struct tags are the single source of truth: `json` for wire+schema, standalone `example:"…"` for swagger examples (NOT `json:"x,example=…"` — swag ignores that form), `validate`/`binding` for rules enforced by `Bind` — keep validation out of handler code
- Logging: `src/common/logs` wraps zap sugared logger as a package-level default — call `logs.LoadLogger()` once in `init()` (already in main), then short calls anywhere: `logs.Infow(msg, kv…)`, `logs.Error(err, msg, kv…)`, `logs.Fatalf(…)`, `LOG_LEVEL` env overrides level; before `LoadLogger` the default is nop so tests stay silent — never log secrets (keys, passwords)
- Comments are in Vietnamese — match the existing style
- Hạn chế tự code mới — có thư viện ngon thì kéo về dùng: HTTP Ollama dùng `ollama/ollama/api` (không tự viết http client), agent/tool dùng Eino (`adk`, `components/tool/utils.InferTool`, `components/model`), chunking dùng `eino-ext` splitters; chỉ tự viết adapter mỏng khi lib thiếu đúng ý (Vd Bearer key Cloud qua Transport, user_id qua ctx vì không tin LLM)
- Swagger: general API info annotation sits above `package main` in cmd/apis/main.go; per-endpoint annotations on handler methods (`@Success 200 {object} dtos.ResponseResource`) — swag resolves type refs via package/search-dir parsing, so handlers just use `:=` (no need to import the DTO type in the handler file); don't use import aliases or full `github.com/x/y.Type` paths in annotations (unsupported/invalid refs)

## Gotchas

- `gofmt -l` flags pre-existing files (`config.go`, `qdrant/connection.go`) only for CRLF line endings (content is formatted) — don't mass-convert line endings
- `configs/config.json` points at Docker-network hosts (`postgres`, `seaweedfs-s3`) — hosts only resolve inside Docker, and since the fx.Invoke above the app now **fails at startup** if DB is down (run Docker, or switch host to `127.0.0.1` locally); the postgres test falls back to `127.0.0.1` since containers publish 5432; dev port is 9001 because Docker Desktop occupies 8080/18080 on this machine
- `.gitignore` cover `.idea`, `*.exe` (built binaries như `ki.exe`), `data/`, `.vscode/`, `.codegraph/` (index máy-local, regen bằng `codegraph sync`) — đừng commit file build thủ công khác
