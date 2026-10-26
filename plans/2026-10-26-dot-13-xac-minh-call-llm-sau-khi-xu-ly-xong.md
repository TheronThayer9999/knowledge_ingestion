# Đợt 13 — Xác minh "UI báo xử lý xong mà vẫn gọi LLM" (provider 9router)

## Bối cảnh

- Người dùng vẫn để `default_provider = 9router` và muốn kiểm tra lại chuyện: **dữ liệu đã xử
  lý xong (UI báo) nhưng log vẫn gọi LLM**.
- Đợt 11 đã chứng minh bằng code + DB rằng LLM chỉ được gọi ở phase **compile** (1 call/mục) và
  ở chat; phase 0 chỉ OCR, phase 1 chỉ bge-m3. Nhưng bằng chứng đó là **tĩnh** (đọc code + đếm
  cột DB). Đợt 12 đo API thật nhưng **cố ý không chạy worker**, nên chưa có chứng cứ **theo thời
  gian** trong cùng một lần chạy: LLM được gọi sau mốc UI báo xong.
- Dữ liệu hiện tại (`manager_db`): 30 bài `done/done/done`, 6 bài `pending/pending/pending`,
  và **1 bài id 47 `tmp_big_policy` (user 37): extract=done, index=done, compile=pending,
  compile_attempts=2, 120 trang, 723 chunk** — đúng mẫu "nội dung đã xử lý xong mà LLM còn chạy".
- UI hiện tại (đọc từ FE):
  - Danh sách tài liệu: badge **chỉ ẩn khi CẢ 3 phase done** (`articleStage` trong
    `knowledge.models.ts`), nhãn `Đã xử lý` = `DOC_STATUS_DONE`.
  - Dialog upload: dòng file báo **"Xong"** ngay khi upload + `POST /articles` thành công —
    mốc này **chưa** liên quan pipeline (extract/index/compile còn `pending`).

## Mục tiêu

1. Chạy worker thật với `default_provider = 9router` để lấy **timeline có timestamp**:
   extract → index (bge-m3) → compile (LLM 9router).
2. Đối chiếu timeline đó với trạng thái UI tại **cùng thời điểm** (poll `/pipeline/articles` +
   luật `articleStage`) để trả lời chính xác: UI báo "xong" ở mốc nào, LLM bắt đầu ở mốc nào.
3. Kết luận: đây là **thiết kế** (compile là phase riêng, không phải bug) hay **UI báo sớm**
   (bug) — kèm đề xuất nếu lệch.

## Phần A — Sự thật code (không sửa gì)

- `cmd/worker` chạy 2 phase ingest (`extract` 30s, `index` 10s) + `compile` 30s, độc lập nhau.
- Compile claim: `ClaimCompilePending(...)` chỉ nhận bài **extract+index done, compile chưa done**
  và chưa hết lease ⇒ bài vừa index xong sẽ được compile ở kỳ quét kế tiếp, **không** chờ gì thêm.
- Mỗi mục có summary → 1 call LLM (`compile: gọi LLM tóm tắt mục`, log đợt 11), cache Redis 30
  ngày. Vì vậy số call LLM = số mục chưa có cache.
- `/pipeline/articles` trả 4 cột `id, extract_status, index_status, compile_status` — UI gộp
  bằng `articleStage`, nên "Đã xử lý" chỉ xuất hiện khi compile xong.

## Phần B — Chạy thật (9router)

1. Bật worker: `go run ./cmd/worker -configs ./configs/config.local.json`
   (default 9router, embedding ollama `bge-m3:latest` ở `localhost:11434`, VLM off).
2. Mẫu A — bài **47**: extract/index đã done từ 22/10 ⇒ kỳ vọng **chỉ** có dòng
   `compile: gọi LLM tóm tắt mục` (chứng minh LLM chạy dù "dữ liệu đã xử lý xong").
3. Mẫu B — bài **63/64** (`API Test A/B`, txt nhỏ, đang pending cả 3): chạy đủ chuỗi
   extract → index → compile, ghi mốc thời gian từng phase.
4. Bằng chứng thu thập:
   - log worker (timestamp) của `extract:`, `index: gọi embed`, `index: embed xong`,
     `compile: gọi LLM tóm tắt mục`, `compile: xong 1 kỳ quét`;
   - trạng thái qua `GET /api/v1/pipeline/articles` poll ~2s trong lúc chạy;
   - DB sau chạy: `extracted_at`, `indexed_at`, `compiled_at`, `compile_status`,
     số dòng `article_sections` của 47/63/64.
5. Kỳ vọng kiểm chứng: mốc `extract=done` + `index=done` xuất hiện **trước** các dòng gọi LLM,
   và với bài 47 hai mốc đó đã có từ nhiều ngày trước.

## Phần C — Đánh giá

Trong `## Kết quả`: bảng timeline (mốc UI ↔ mốc LLM), kết luận đúng thiết kế hay lệch, và nếu
lệch thì đề xuất cụ thể (nhãn UI theo phase, thứ tự hiển thị, log mỗi call kèm provider).

## Kiểm chứng

Không đổi code Go ⇒ không cần build/lint/test lại; chỉ cần log + DB + API thật. Tắt worker sau
khi xong (API :9001 vẫn để chạy vì FE đang dùng).

## Commit

1. `docs(plans): lên kế hoạch đợt 13`
2. `docs(plans): kết quả xác minh call LLM sau khi xử lý xong`

## Ngoài phạm vi

- Không sửa UI/nhãn i18n (nếu cần đổi cách hiển thị phase thì tách đợt 14).
- Không đổi `default_provider`, không sửa `config.local.json`.
- Bài 47 sẽ được compile thật (đó là chạy đúng chức năng, không phải chỉnh dữ liệu).
