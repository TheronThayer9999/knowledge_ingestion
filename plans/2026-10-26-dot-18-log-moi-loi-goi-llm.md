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

(chờ chạy xong)
