# Đợt 18 — Log MỌI lời gọi LLM (kèm phase) + bằng chứng worker đã hết việc

## Vấn đề

UI báo tài liệu "xong" nhưng mở dashboard 9router vẫn thấy request LLM mới. Hiện
log **không đủ để đối chiếu**: chỉ có log ở tầng service (`compile: gọi LLM tóm tắt
mục`, `chat: gọi agent`), còn tầng HTTP thì im lặng. Trong khi đó:

- 1 lượt chat = **nhiều** request: 1 lần classify (`chat: label`) + N vòng tool của
  agent + có thể 1 lượt retry (follow-up 0 hit / answer rỗng) + 1 lần tóm tắt lịch
  sử khi hội thoại dài. Dashboard 9router đếm **request HTTP**, log của mình đếm
  **lượt gọi hàm** ⇒ số lệch nhau, không biết request nào thuộc phase nào.
- Phase compile cũng gọi 9router (vì `llms.default_provider = 9router` bên
  `configs/config.local.json`) nên khi còn bài khác đang compile, dashboard vẫn
  nhảy dù bài đang xem đã done.
- Không có con số "kỳ quét này có gọi LLM hay không" ⇒ không chứng minh được worker
  đã im lặng sau khi bài xong.

## Thay đổi

| File | Việc |
|---|---|
| `src/common/constants/llm_site.go` (mới) | Hằng nhãn phase gọi LLM: `LLMSiteCompile`, `LLMSiteChat`, `LLMSiteClassify` |
| `src/common/utils/llm.go` (mới) | `WithLLMSite(ctx, site)` / `LLMSiteFromCtx(ctx)` — nhãn đi theo ctx xuống tận HTTP |
| `src/agent/eino/model.go` | Thêm `loggingTransport` bọc NGOÀI CÙNG mọi client (ollama + openai-compatible): mỗi request thật 1 dòng `llm: request` (site, provider, model, host, path, trace_id) + 1 dòng `llm: request xong` (status, elapsed_ms), lỗi thì `logs.Error` |
| `src/controller/services/article_compile_service.go` | Gắn site `compile`; đếm `llm_calls`/`cache_hits` theo từng bài và theo kỳ; log `reclaimed` khi nhận lại bài `processing`; warn lúc dựng nếu không có Redis; dòng `pipeline: bài xong cả 3 phase (UI báo done)` ngay sau `MarkCompileDone`; hạ `compile: bắt đầu kỳ quét` xuống Debug |
| `src/agent/usecase/chat_service.go` | Gắn site `chat` cho `agent.Run`, `classify` cho `label` |
| `src/agent/usecase/search_service.go` | Gắn site `classify` cho vòng 1 |

## Vì sao làm ở transport (không phải thêm log ở từng chỗ gọi)

Transport là **cửa duy nhất** mọi request LLM đi qua: agent loop, middleware tóm tắt
lịch sử, classifier, summarizer compile đều dùng chung `http.Client` của provider.
Bọc ở đây thì không thể quên chỗ nào, kể cả code thư viện (Eino/adk) tự gọi model.
`site` truyền bằng ctx nên không phải sửa chữ ký hàm nào của thư viện.

## Không làm

- Không giảm nhịp poll (`COMPILE_INTERVAL` 30s) — poll rẻ, và dòng log cuối kỳ
  chính là bằng chứng "worker sống nhưng không gọi LLM".
- Không tắt/đổi cache Redis, không đổi prompt, không đổi `llms.default_provider`.
- Không log nội dung prompt (có thể chứa dữ liệu tài liệu) — chỉ log kích thước.

## Kiểm chứng

1. `go build ./... && go vet ./... && golangci-lint run ./...`, `go test ./...`.
2. E2E worker thật với `configs/config.local.json` (9router): reset 1 bài đã compile
   (`compile_status=pending`, xoá `article_sections`) + xoá key cache `compile:sum:*`
   của bài ⇒ mỗi mục phải sinh **đúng 1** dòng `llm: request {site=compile}` và
   `compile: gọi LLM tóm tắt mục` khớp số.
3. Sau khi bài `done`: chạy thêm ≥2 kỳ quét, mỗi kỳ phải có
   `compile: xong 1 kỳ quét {found=0, compiled=0, llm_calls=0}` và **không** còn dòng
   `llm: request` nào ⇒ chứng minh worker không gọi LLM sau khi UI báo xong. Nếu
   dashboard 9router vẫn nhảy thì request đó thuộc site khác (chat/classify) hoặc
   process khác — cả hai đều nhìn thấy được trong log.
4. Đối chiếu DB: `compile_status/compiled_at` của bài test.

## Kết quả

### Kiểm tra tĩnh

`go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), `go test ./...`
đều xanh. `src/quality` vẫn qua ngân sách hàm (60 dòng) sau khi tách `compileOne`
thành `compileOne` + `buildRows` + `count` + `compileStats`.

### E2E worker thật (9router, `configs/config.local.json`)

Bài test: `48 - triethoc_tay_phuong` (5 mục), `compile_status=pending`, không có row
`article_sections`, cache `compile:sum:*` rỗng (0 key).

**Lượt 1 — cache lạnh** (log thật, rút gọn):

```
compile: nhận lô bài      {count: 1, reclaimed: 0, articles: ["48:triethoc_tay_phuong (...)"]}
compile: gọi LLM tóm tắt mục {article_id: 48, section: "ĐIỀU 5: ...", chars: 888, model: kc/nvidia/...}
llm: request              {site: compile, provider: 9router, model: kc/nvidia/..., host: localhost:20128, path: /v1/chat/completions, trace_id: 01a17710-a7d6-...}
llm: request xong         {site: compile, status: 200, elapsed_ms: 7778, trace_id: 01a17710-a7d6-...}
compile: xong 1 bài       {article_id: 48, sections: 5, llm_calls: 5, cache_hits: 0, elapsed_ms: 223157}
pipeline: bài xong cả 3 phase (UI báo done) {article_id: 48, compiled_at: 2026-10-26T14:19:25+07:00}
compile: xong 1 kỳ quét   {found: 1, compiled: 1, llm_calls: 5, cache_hits: 0}
```

Đúng 5 mục ⇒ đúng 5 dòng `llm: request` (1:1). Trong đó có 1 lần 9router trả 503:

```
WARN llm: dính giới hạn, thử lại {provider: 9router, attempt: 1, status: 503, wait_ms: 30000}
```

Đây là lý do **dashboard provider đếm nhiều hơn số lượt gọi logic**: mỗi lần retry là
1 request thật sang provider, và log của mình lộ ra đúng chỗ đó.

**Sau mốc UI báo done: KHÔNG còn request nào.** Quét phần log sau dòng
`pipeline: bài xong cả 3 phase`: 55 dòng, `llm: request` = **0**, và ≥2 kỳ quét liên
tiếp `compile: xong 1 kỳ quét {found: 0, compiled: 0, llm_calls: 0, cache_hits: 0}`.

**Lượt 2 — cache nóng** (reset bài về pending, giữ nguyên 5 key cache):

```
compile: xong 1 bài     {article_id: 48, sections: 5, llm_calls: 0, cache_hits: 5}
compile: xong 1 kỳ quét {found: 1, compiled: 1, llm_calls: 0, cache_hits: 5}
```

Số đếm khớp đúng thực tế (không gọi LLM lần nào, 5 mục lấy từ cache).

### Vì sao trước đây "UI báo xong mà 9router vẫn thấy call"

1. `llms.default_provider = 9router` ⇒ **compile, chat, classify dùng chung một provider**.
   Một lượt chat sinh nhiều request (classify + N vòng tool + retry + tóm tắt lịch sử),
   nên request trên dashboard không nhất thiết của tài liệu đang xem — giờ nhìn `site`
   là biết ngay.
2. Bài khác còn đang compile (worker xử lý 2 bài/lô, 30s/kỳ) — UI chỉ báo theo từng bài.
3. Row `compile_status=processing` bị **nhận lại** khi lease hết hạn (30m). Đã bắt được
   đúng ca này trong DB dev: bài `47 - tmp_big_policy` kẹt `processing`, `attempts=2`,
   `compile_next_retry_at` đã qua ⇒ mỗi kỳ quét lại compile lại từ đầu. Log mới in
   `reclaimed` trong dòng `compile: nhận lô bài` để ca này không còn vô hình.
   (Trong lúc E2E tôi dừng worker giữa chừng nên chính bài 48 cũng từng về `processing`.)
4. Retry 429/5xx (Retry-After tới 60s) nhân số request phía provider.

### Dọn dẹp

- Worker dev đã dừng, xoá `dot18_worker.exe` + log tạm.
- DB dev: bài 48 `done`; bài 47 (`tmp_big_policy`) đã dời `compile_next_retry_at` +2 ngày
  để không chiếm lô trong lúc test — cần compile lại thì set về `now()`.
