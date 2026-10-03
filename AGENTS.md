# AGENTS.md

## Commands

- Verify: `go build ./...` && `go vet ./...` — no lint config, no CI, no Makefile, no test files exist
- Run: `go run ./cmd` → `GET /api/v1/ping`, `GET /api/v1/pong`, Swagger UI at `/swagger/index.html`
- Config: `-configs <path>` flag, default `./configs/config.json`; `APP_CONFIG_PATH` env overrides the flag default
- Swagger: regenerate after changing annotations with `swag init -g cmd/main.go` (swag CLI in `%USERPROFILE%\go\bin`); `docs/` is generated — never hand-edit it
- `go mod tidy` after adding/removing imports (it drops unused deps like gorm/pgx)

## Architecture

- Go 1.26.5, single module `knowledge_ingestion`; import paths are `knowledge_ingestion/src/...` — keep this prefix, all code lives under `src/`
- Entrypoint `cmd/main.go`: `fx.New(Provide(config), Options(loader.Load()...), Invoke(serverLifecycle))` then manual `Start` → OS signal → `Stop` (not `fx.Run`)
- All DI wiring lives in `src/loader/loader.go` — `Load() []fx.Option` grouped as loadAdapter/loadService/loadValidator/loadEngine (same pattern as the shoe_shop fxloader); add new providers there only, never as package-level `var Module`
- Layer flow: `routers` (route table only, exposes `Engine`; HTTP server lifecycle is in `serverLifecycle` in cmd/main.go) → `apis` (gin handlers) → `services` (business logic behind `I…` interfaces) → `domain/dtos`; config injected everywhere via `config.IConfig`
- Empty scaffold dirs reserved for future use: `common/{logs,utils}`, `controller/middlewares`, `domain/models`, `repository`, `infrastructure/seaweedfs` — follow the existing layer pattern when filling them- `infrastructure/postgres/connection.go` is NOT wired (commented in `loadAdapter`); `sql.Open("postgres", ...)` has no driver registered — import `lib/pq` (driver name `postgres`) or switch to `pgx/v5/stdlib` (driver name `pgx`) and uncomment before enabling it

## Conventions

- Interfaces prefixed `I` (IConfig, IPingPongService, IDB); constructors take interfaces, fx binds interface → concrete (SOLID/DIP)
- Services never return bare values/errors — they return `dtos.Result[T]` built with `dtos.Ok(data)` / `dtos.Fail(err)`; handlers map it in one call: `Render(c, a.baseController, res)`
- Handlers embed `*baseController` (`apis/base.go`): `Success`/`ErrorData`/`BadRequest` emit the `{status, code, message, data}` envelope (`dtos.ResponseResource`), `Bind` = bind + validate — never return raw JSON from a handler
- Custom errors live in `src/common/errors`: `errors.Error` carries `Code` (body) + `HttpCode` (status line); build with `NewCustomHttpError`, unwrap with `errors.From`, sentinels `Success/BadRequest/Internal` + `ErrBadRequest/ErrInternal` — the package name shadows stdlib `errors`, so alias stdlib when a file needs both
- Struct tags are the single source of truth: `json` for wire+schema, standalone `example:"…"` for swagger examples (NOT `json:"x,example=…"` — swag ignores that form), `validate`/`binding` for rules enforced by `Bind` — keep validation out of handler code
- Comments are in Vietnamese — match the existing style
- Swagger: general API info annotation sits above `package main` in cmd/main.go; per-endpoint annotations on handler methods reference response types from `domain/dtos` (handlers declare `var resp dtos.X = ...` so the import resolves)

## Gotchas

- `gofmt -l` flags several pre-existing files (`config.go`, `connection.go`, `qdrant/connection.go`, `storage.go`) only for CRLF line endings (content is formatted) — don't mass-convert line endings
- `configs/config.json` points at Docker-network hosts (`postgres`, `seaweedfs-s3`) — fine to boot locally since DB is not wired yet; dev port is 9001 because Docker Desktop occupies 8080/18080 on this machine
- `.gitignore` is empty — don't commit built binaries (`ki.exe`, etc.)
