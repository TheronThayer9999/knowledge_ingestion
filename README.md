# Knowledge Ingestion

Personal knowledge-ingestion service: users upload documents (files or web links), organize them in a private category tree, with blobs on SeaweedFS (S3-compatible) + metadata in Postgres — feeding a downstream embedding/search pipeline (Qdrant + Ollama infra already in place).

Stack: **Go 1.26.5** · Gin · GORM · fx (DI) · JWT · SeaweedFS S3 · Postgres · Redis · Qdrant · Swagger.

## Features

- **JWT auth**: register/login, password change (instantly invalidates old tokens), account lockout. Every business route sits behind `RequiredAuth`.
- **Categories**: single-root taxonomy tree **per user** (owner taken from the token) — CRUD, sub-categories, parent-with-children delete blocked.
- **Articles**: each belongs to one of my categories, with exactly one source:
  - `url` — external web link (fetched later by a worker),
  - `storage_key` — file already uploaded to S3 via presign; the server verifies it with `HeadObject` before saving the row; deleting an article also cleans up its blob.
- **Presigned uploads**: client requests a ticket (`POST /uploads/presign`, batch up to 20 files) → PUTs straight to S3, bytes never touch the server. The server enforces an extension whitelist + magic-bytes sniffing and generates UUID keys.
- **Absolute ownership**: every query is scoped by `user_id` — other users' data returns 404/empty as if it didn't exist.

## Architecture

```
routers (route table, exposes Engine)
  → apis (gin handlers: Bind/BindUri/BindQuery + Render, no DTO mapping)
    → services (business logic, return dtos.Result[T], behind I… interfaces)
      → domain models + controller/dtos · repository (domain models only)
  → infrastructure (postgres / seaweedfs / redis / embedding / qdrant)
```

- DI with **fx**, all wiring in `src/loader/loader.go` (grouped as `loadAdapter/loadService/loadMiddleware/loadValidator/loadEngine`). Interfaces prefixed with `I`, constructors take interfaces (SOLID/DIP).
- The authenticated identity flows from middleware into services via **`ICurrentUser`** (reads `user_id` from the request context, .NET `IHttpContextAccessor` style) — services never see gin.
- Errors: `common/errors.Error` carries `Code` (body) + `HttpCode`; services wrap client-visible errors with `NewCustomHttpError`, everything else becomes a 500 via `Render` without leaking internals.
- Config (`-configs` flag, default `./configs/config.json`, `APP_CONFIG_PATH` env overrides the flag) is injected everywhere via `config.IConfig`.
- DB `AutoMigrate` + S3 **bucket ensure** run at startup — **fail-fast** if Postgres/SeaweedFS is unreachable (run Docker, or point hosts to `127.0.0.1` for local dev).

## Quickstart

```powershell
# 1. Infra (provide postgres yourself at 127.0.0.1:5432 per configs/config.json)
docker compose -f docker/seaweed_fs_compose/docker-compose.yml up -d
docker compose -f docker/redis_docker-compose/docker-compose.yml up -d

# 2. App (port 9001, Swagger UI at /swagger/index.html)
go run ./cmd/apis
```

End-to-end file upload (get a token via `POST /api/v1/users` → `POST /api/v1/auth/login` first):

```powershell
# 1. Request a ticket
curl.exe -X POST http://localhost:9001/api/v1/uploads/presign `
  -H "Authorization: Bearer <token>" -F "file=@./paper.pdf"
# → {upload_url, key, content_type, expires_at (15 minutes)}

# 2. PUT straight to S3 (Content-Type must match the signed one)
curl.exe -X PUT "<upload_url>" -H "Content-Type: <content_type>" --data-binary "@./paper.pdf"

# 3. Create the article (server verifies the key with HeadObject, then saves)
curl.exe -X POST http://localhost:9001/api/v1/articles `
  -H "Authorization: Bearer <token>" -H "Content-Type: application/json" `
  -d '{"storage_key":"<key>","name":"Paper","content_type":"<content_type>","category_id":1}'
```

## API

| Method | Route | Auth | Description |
|---|---|---|---|
| GET | `/api/v1/ping`, `/pong` | No | Health check |
| POST | `/api/v1/users` | No | Register |
| POST | `/api/v1/auth/login` | No | Login, returns JWT |
| GET | `/api/v1/users` | Yes | List users |
| GET | `/api/v1/auth/profile` | Yes | My profile |
| POST | `/api/v1/auth/change-password` | Yes | Change password (revokes old tokens) |
| POST | `/api/v1/uploads/presign` | Yes | Ticket for 1 file |
| POST | `/api/v1/uploads/presign/batch` | Yes | Tickets for up to 20 files |
| POST/GET | `/api/v1/categories` | Yes | Create / list my categories |
| GET/PUT/DELETE | `/api/v1/categories/:id` | Yes | Detail / partial update / delete |
| POST | `/api/v1/articles` | Yes | Create article (`url` **or** `storage_key`, exactly one) |
| GET | `/api/v1/articles?category_id=\|category_name=&limit=&offset=` | Yes | List by exactly one filter |
| GET/PUT/DELETE | `/api/v1/articles/:id` | Yes | Detail / partial update / delete (+blob cleanup) |

Full request/response shapes: Swagger UI at `/swagger/index.html` (regenerate with `swag init -g cmd/apis/main.go` after changing annotations; `docs/` is generated, never hand-edited).

## Dev commands

```powershell
go build ./...; go vet ./...; golangci-lint run ./...  # verify (no .golangci.yml on purpose)
go test ./...                                          # seaweedfs, postgres, apis...
go mod tidy                                            # after adding/removing imports
```

## Key conventions (see `AGENTS.md` for the full list)

- DTOs in `src/controller/dtos`: `<Action><Entity>Request` / `<Entity>Response`, `snake_case` JSON, validation in `binding` tags, pointer fields for partial updates, hand-written mappers in the same package.
- Struct tags are the single source of truth: `json` (wire), standalone `example` (swagger), `binding` (validation).
- Log via `common/logs` (zap), never log secrets. Comments are in Vietnamese.
- `gofmt -l` flags a few pre-existing CRLF files (`config.go`, `qdrant/connection.go`) — content is formatted, don't mass-convert line endings.
