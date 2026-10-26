# Đợt 12 — Provider NVIDIA cho BE + test API thực tế

## Bối cảnh

- Người dùng đã thêm provider **nvidia** vào `configs/config.local.json` (file gitignored,
  chứa key thật): `type=openai`, `base_url=https://integrate.api.nvidia.com/v1`,
  `model=meta/muse-glimmer-30b`, `timeout_sec=300`.
- `configs/config.json` (file commit, dùng làm template) **chưa có** entry này ⇒ máy khác
  clone về không thấy provider; đồng thời quy ước "không commit credential" nên bản
  trong repo phải để `api_key` rỗng.
- Song song: chạy **test API thực tế** (server thật, DB thật, LLM thật) và đánh giá —
  trọng tâm là provider mới + luồng upload nhiều file vừa làm ở đợt 10.

Môi trường đang chạy: postgres 5432, qdrant 6333/6334, seaweedfs-s3 8333, redis 6379,
9router 20128, ocr-pdf 9008. API 9001 **chưa chạy** (sẽ tự bật để test).

## Phần A — Config BE

Thêm vào `llms.providers` của `configs/config.json`:

```json
"nvidia": {
  "type": "openai",
  "model": "meta/muse-glimmer-30b",
  "base_url": "https://integrate.api.nvidia.com/v1",
  "api_key": "",
  "temperature": 0,
  "max_tokens": 4096,
  "timeout_sec": 300
}
```

- Giữ `default_provider` nguyên (local: `9router`).
- Không đổi code: `type=openai` + `base_url` đã đủ để `eino.NewConfigProvider` dựng client
  (NVIDIA NIM dùng OpenAI-compatible `/chat/completions`).
- Kiểm tra config parse được và provider xuất hiện: `go run ./cmd/worker -list-llms -configs ./configs/config.json`.

## Phần B — Test API thực tế

Bật `go run ./cmd/apis` (đọc `configs/config.local.json`), rồi:

| # | Case | Kỳ vọng |
|---|---|---|
| 1 | `GET /api/v1/ping` | 200 |
| 2 | `GET /health` | 200, DB/Qdrant ok |
| 3 | `POST /api/v1/auth/login` (feuser) | 200 + access_token |
| 4 | `GET /api/v1/categories` không token | 401 |
| 5 | `GET /api/v1/categories` có token | 200, danh sách |
| 6 | `POST /uploads/presign/batch` 2 file txt (multipart) | 200, 2 `items` đúng thứ tự |
| 7 | `PUT` blob từng vé lên SeaweedFS | 200 |
| 8 | `POST /articles` × 2 | 201, 2 bài gắn đúng `category_id` |
| 9 | `GET /articles?category_id=…` | 200, thấy 2 bài mới |
| 10 | `POST /api/v1/chat` `provider=nvidia`, model trong config | 200 + answer; nếu lỗi thì đọc đúng mã lỗi |
| 11 | `POST /api/v1/chat` `provider=nvidia` + model NIM hợp lệ khác | tách lỗi "sai tên model" vs "sai base_url/key" |
| 12 | `POST /api/v1/chat` mặc định (9router) | 200 |
| 13 | `GET https://integrate.api.nvidia.com/v1/models` (Bearer key) | danh sách model thật của tài khoản |

Công cụ: `apiprobe` MCP (`http_request`/`api_test`/`save_auth`) cho các endpoint JSON;
`curl.exe` cho multipart (`-F files=@…`) vì tool MCP không dựng được multipart.

## Phần C — Đánh giá

Trong `## Kết quả`: bảng pass/fail, mã lỗi thật, nhận xét về
- provider nvidia (model mặc định có tồn tại trên NIM không, timeout, chi phí),
- luồng batch presign + tạo bài,
- rủi ro còn lại (key trong `config.local.json`, `default_provider` trỏ 9router local).

## Kiểm chứng

`go build ./...`, `go vet ./...`, `golangci-lint run ./...`, `go test ./...` (config không
đổi code nên chỉ cần chắc config parse); `-list-llms` in ra `nvidia`; DB check bằng psql
cho 2 bài vừa tạo; tắt server sau khi test.

## Commit

1. `docs(plans): lên kế hoạch đợt 12`
2. `chore(config): thêm provider nvidia cho BE`
3. `docs(plans): kết quả test API đợt 12`

## Ngoài phạm vi

- Không đổi `default_provider` sang `nvidia` (chat mặc định vẫn 9router).
- Không commit key thật; không sửa `config.local.json` của người dùng.
- Không chạy worker để OCR/index/compile 2 file test (tốn LLM; chỉ test tới bước tạo bài).
