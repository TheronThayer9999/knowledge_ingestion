# AGENTS.md

## Commands

- Verify: `go build ./...` && `go vet ./...` — no lint config, no CI, no Makefile; tests: `go test ./...` (`seaweedfs`, `postgres`, `apis`)
- Run: `go run ./cmd` → `GET /api/v1/ping`, `GET /api/v1/pong`, Swagger UI at `/swagger/index.html`
- Config: `-configs <path>` flag, default `./configs/config.json`; `APP_CONFIG_PATH` env overrides the flag default
- Swagger: regenerate after changing annotations with `swag init -g cmd/main.go` (swag CLI in `%USERPROFILE%\go\bin`); `docs/` is generated — never hand-edit it
- `go mod tidy` after adding/removing imports (it aggressively drops unused deps — it removed gorm/pgx once when nothing imported them)

## Architecture

- Go 1.26.5, single module `knowledge_ingestion`; import paths are `knowledge_ingestion/src/...` — keep this prefix, all code lives under `src/`
- Entrypoint `cmd/main.go`: `fx.New(Provide(config), Options(loader.Load()...), Invoke(serverLifecycle))` then manual `Start` → OS signal → `Stop` (not `fx.Run`)
- All DI wiring lives in `src/loader/loader.go` — `Load() []fx.Option` grouped as loadAdapter/loadService/loadValidator/loadEngine (same pattern as the shoe_shop fxloader); add new providers there only, never as package-level `var Module`
- Layer flow: `routers` (route table only, exposes `Engine`; HTTP server lifecycle is in `serverLifecycle` in cmd/main.go) → `apis` (gin handlers) → `services` (business logic behind `I…` interfaces) → `domain/dtos`; config injected everywhere via `config.IConfig`
- Storage: contract `common/storage.IStorage` (`Save`/`Open`/`Delete`), implemented by `infrastructure/seaweedfs` with aws-sdk-go-v2 against the S3-compatible gateway (path-style, static creds, region fixed `us-east-1`); provided in `loadAdapter` — fx builds it lazily, so the app boots without SeaweedFS running
- DB: `infrastructure/postgres.NewConnection` (wired in `loadAdapter`) uses gorm (`gorm.io/gorm` + `gorm.io/driver/postgres`) and returns `postgres.IDB` (`GetDB() *gorm.DB`); pool tuned 25 open/10 idle/5m lifetime, ping fail-fast 10s — but fx is lazy, so the app still boots with Docker off; `connection_test.go` dials the config host, falls back to `127.0.0.1`, skips when unreachable
- Empty scaffold dirs reserved for future use: `common/utils`, `controller/middlewares`, `repository` — follow the existing layer pattern when filling them

## Conventions

- Interfaces prefixed `I` (IConfig, IPingPongService, IDB); constructors take interfaces, fx binds interface → concrete (SOLID/DIP)
- Services never return bare values/errors — they return `dtos.Result[T]` built with `dtos.Ok(data)` / `dtos.Fail(err)`; handlers map it in one call: `Render(c, a, res)` (Render takes the `renderer` interface — any controller embedding `*baseController` satisfies it, so no need to name the struct). Render maps `*errors.Error` → its own HttpCode/Code; any other error → `logs.Error` + `ErrInternal` (500), never a bare 400 and never leaking internals — so services wrap client-visible errors with `NewCustomHttpError` and let everything else stay plain
- `domain/models` are gorm persistence models: embed `BaseModel` by value (ID/DeletedAt/CreatedAt/UpdatedAt); `DeletedAt` is `gorm.DeletedAt` (soft delete) tagged `json:"-"` (gorm.DeletedAt has no custom marshaller, JSON would be `{Time,Valid}`), secrets like `Password` are `json:"-"` too
- Handlers embed `*baseController` (`apis/base.go`): `Success`/`ErrorData`/`BadRequest` emit the `{status, code, message, data}` envelope (`dtos.ResponseResource`), `Bind` = bind + validate — never return raw JSON from a handler
- Custom errors live in `src/common/errors`: `errors.Error` carries `Code` (body) + `HttpCode` (status line); build with `NewCustomHttpError`, unwrap with `errors.From`, sentinels `Success/BadRequest/Internal` + `ErrBadRequest/ErrInternal` — the package name shadows stdlib `errors`, so alias stdlib when a file needs both
- Struct tags are the single source of truth: `json` for wire+schema, standalone `example:"…"` for swagger examples (NOT `json:"x,example=…"` — swag ignores that form), `validate`/`binding` for rules enforced by `Bind` — keep validation out of handler code
- Logging: `src/common/logs` wraps zap sugared logger as a package-level default — call `logs.LoadLogger()` once in `init()` (already in main), then short calls anywhere: `logs.Infow(msg, kv…)`, `logs.Error(err, msg, kv…)`, `logs.Fatalf(…)`, `LOG_LEVEL` env overrides level; before `LoadLogger` the default is nop so tests stay silent — never log secrets (keys, passwords)
- Comments are in Vietnamese — match the existing style
- Swagger: general API info annotation sits above `package main` in cmd/main.go; per-endpoint annotations on handler methods (`@Success 200 {object} dtos.ResponseResource`) — swag resolves type refs via package/search-dir parsing, so handlers just use `:=` (no need to import the DTO type in the handler file); don't use import aliases or full `github.com/x/y.Type` paths in annotations (unsupported/invalid refs)

## Gotchas

- `gofmt -l` flags pre-existing files (`config.go`, `qdrant/connection.go`) only for CRLF line endings (content is formatted) — don't mass-convert line endings
- `configs/config.json` points at Docker-network hosts (`postgres`, `seaweedfs-s3`) — app boots fine locally (fx lazy, DB ping only on first use) but the hosts only resolve inside Docker; the postgres test falls back to `127.0.0.1` since containers publish 5432; dev port is 9001 because Docker Desktop occupies 8080/18080 on this machine
- `.gitignore` is empty — don't commit built binaries (`ki.exe`, etc.)
