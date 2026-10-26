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

## Kết quả

### Phần A — Config

- `configs/config.json`: thêm provider `nvidia` (`api_key: ""`) — commit `ca48967`.
- `go run ./cmd/worker -configs ./configs/config.json -list-llms` →
  `nvidia type=openai key=missing model=meta/muse-glimmer-30b` (parse OK, key rỗng đúng quy ước).
- Kiểm tra thật trên NIM (Bearer key trong `config.local.json`): `GET /v1/models` → 200,
  **85 model**, có `meta/muse-glimmer-30b` ⇒ tên model trong config là thật.
- Không phải sửa code: `type=openai` + `base_url` là đủ.

### Phần B — Bảng test (server thật :9001, config.local.json, DB thật)

| # | Case | KQ | Bằng chứng |
|---|---|---|---|
| 1 | `GET /ping` | PASS | 200, 0ms |
| 2 | `GET /health` | PASS | 200, 75–160ms |
| 3 | `POST /auth/login` (feuser) | PASS | 200, 70–90ms, có `token` |
| 4 | `GET /categories` không token | PASS | 401 `thiếu token xác thực` |
| 5 | `GET /categories` có token | PASS | 200 |
| 6 | `POST /uploads/presign/batch` 2 file | PASS | 200, 4.8ms, 2 `items` **đúng thứ tự** gửi, key `uploads/<uuidv7>.txt`, `content_type=text/plain`, hết hạn 15' |
| 7 | `PUT` 2 vé lên SeaweedFS | PASS | 200, 200 |
| 8 | `POST /articles` × 2 | PASS | 200; 63 `API Test B`, 64 `API Test A`, `category_id=124`, `storage_key` = key trả ở #6 |
| 9 | `GET /articles?category_id=124` | PASS | 200, thấy đúng 2 bài |
| 9b | `POST /articles` với `storage_key` rác | PASS | 400 `file chưa được upload lên kho` |
| 10 | `POST /chat` `provider=nvidia` | PASS | 200, trả lời tiếng Việt, **12.76s** |
| 11 | `POST /chat` nvidia + model sai | PASS | 400 sau 484ms, `provider từ chối model "meta/khong-ton-tai": … 404 page not found` |
| 12 | `POST /chat` mặc định (9router) | PASS | 200, **4.56s** |
| 13 | `GET https://integrate.api.nvidia.com/v1/models` | PASS | 200, 85 model |
| 14 | `POST /search` | PASS | 200, 231–336ms |
| 15 | `POST /chat` RAG 0 hit | **FAIL (hiệu năng)** | 200 nhưng **65.8s**, 0 `cites` |
| 16 | `POST /chat` RAG provider=nvidia | **FAIL** | 500 sau 5m00s khi client bỏ (log `context canceled`) |

DB sau test: article 63/64 `pending/pending/pending` (worker cố ý không chạy).

### Phần C — Đánh giá

1. **Provider nvidia**: dùng được ngay, không cần code, model có thật trên tài khoản. Nhưng
   **~12.8s/lượt chitchat** so với 4.56s của 9router (≈3× chậm) ⇒ không nên đặt làm
   `default_provider` cho chat; hợp hơn cho worker compile (chạy nền, chấp nhận chậm).
   Mapping lỗi provider đúng như thiết kế (`providerError`): sai model ⇒ 400 kèm tên model.
2. **Batch upload (đợt 10)**: xanh toàn bộ — 1 call presign ra 2 vé đúng thứ tự, PUT thẳng
   lên SeaweedFS, tạo bài nhận đúng key; chốt chặn HeadObject chặn `storage_key` rác.
3. **RAG chat chậm — root cause**: không nằm ở embedding/Qdrant (`/search` chỉ 231–336ms)
   mà ở **vòng agent**: khi search trả 0 hit, agent lặp gọi tool tới `maxIterations=12`
   rồi mới trả lời ⇒ 65.8s (9router), >2' (nvidia) và 500 khi client bỏ ngang.
   Vì sao 0 hit: retrieval scope theo `user_id` (payload `principal_index`). Qdrant có
   **17280 point** nhưng facet `user_id` = {37: 16287, 53: 982, 36: 11}; tài khoản test
   `feuser` = **44 → 0 point**. Tức scope đúng thiết kế, chỉ là account test không sở hữu
   tài liệu nào ⇒ **mọi câu RAG bằng feuser đều 0 hit**.
   Hệ quả UX: user chưa upload gì mà hỏi sẽ chờ ~1 phút để nhận "không tìm thấy".
   Đề xuất (ngoài phạm vi đợt): short-circuit khi lượt search đầu 0 hit (trả lời ngay +
   gợi ý nạp tài liệu), hạ `maxIterations`, và log mỗi tool call/iteration để đếm được
   vòng lặp từ log (hiện chỉ có 1 dòng `chat: gọi agent`/request).
4. **Lệch DB ↔ Qdrant (cần kiểm tra)**: `articles.index_done_chunks` cộng lại ≈ 45.5k chunk
   nhưng Qdrant chỉ có 17.3k point; riêng user 8 (16.654 chunk, 8 bài) **không có point nào**
   trong khi checkpoint vẫn `done`. Nên chạy `POST /admin/rebuild-vectors` cho các user này
   hoặc kiểm tra lại nhánh `DeleteByArticle` khi re-index.
5. **Rủi ro bảo mật**: `configs/config.json` (file commit) **vẫn chứa api_key thật của Ollama
   cloud ở dòng 49** — vi phạm quy ước "config commit để key rỗng", và key đã nằm trong git
   history ⇒ phải rotate rồi set `""`. Không tự sửa vì ngoài phạm vi đợt (chờ người dùng quyết).
6. **Ghi chú công cụ**: apiprobe `http_request` chỉ bind JSON đúng khi gửi kèm
   `headers: {Content-Type: application/json}`; thiếu header thì gin bind form ⇒ 400
   `Query ... required` dù body có.
7. **Lệch kế hoạch có chủ ý**: server :9001 **vẫn để chạy** sau test (kế hoạch ghi "tắt server")
   vì FE đang poll `/articles`, `/pipeline/articles`, `/categories`. Tắt bằng Ctrl+C ở terminal 3.
   Không chạy `codegraph sync` vì đợt này không sửa file Go nào.
