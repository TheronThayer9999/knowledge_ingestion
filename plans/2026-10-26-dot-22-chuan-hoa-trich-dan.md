# Đợt 22 — BE chuẩn hoá trích dẫn: không tin model viết đúng `[^n]`, không để lộ ID kỹ thuật

## Vấn đề

User chụp lại câu trả lời và thấy:

1. **Marker sai khuôn**: model viết `[*2]`, `[*3]` thay vì `[^2]`, `[^3]`. Tool đã
   in rõ chỉ dẫn "PHẢI trích dẫn bằng [^n]" (`knowledge_search.go:72`) và prompt
   §1 cũng viết vậy, nhưng model free vẫn lệch ký tự — `marked` không có footnote
   nên FE in nguyên `[*2]` ra màn hình, trông như lỗi font.
2. **Lộ ID kỹ thuật**: trong answer xuất hiện "Article ID 54", "Article ID 56".
   Prompt §10 cấm hẳn ("Không nhắc đến điểm số, ID kỹ thuật, tên collection…")
   nhưng cấm bằng lời thì model có thể không nghe.
3. **Số ngoài phạm vi**: collector cấp n theo thứ tự thấy lần đầu (`context.go:88`,
   trần 30) và `chat_service.Chat` đánh lại cites là `i+1` — hai bên khớp nhau,
   nhưng model có thể bịa số (hoặc nhắc số của hit bị trần cắt mất, vì `Add` trả
   `n=0` khi vượt trần). Marker trỏ vào số không có trong `cites` thì FE bấm vào
   là hỏng.

Nguyên tắc đã chốt với user: **không để LLM đoán số trích dẫn** — BE là chỗ cuối
chốt lại hình thức marker trước khi answer ra khỏi process.

## Thiết kế

Một hàm thuần `normalizeCitations(answer string, nCites int) string` trong
`src/agent/usecase/citation.go`, gọi đúng một chỗ: `Chat` dựng response.
Không đổi prompt (prompt vẫn là hướng dẫn chính), không đổi collector (n đã tất
định), chỉ sửa text — vì phần này quyết định được bằng dữ kiện có sẵn: danh sách
cites của chính lượt đó.

Ba bước, chạy theo thứ tự:

| Bước | Chi tiết |
|---|---|
| Nhận diện marker | Regex `\[\s*[*^]?\s*(\d{1,3})\s*\]` phủ `[^n]`, `[*n]`, `[n]`, `[ ^ 3 ]`. Không đụng cặp ngoặc vuông có chữ (link/markdown text) — chỉ số mới là marker |
| Chốt phạm vi | `1 <= n <= nCites` thì giữ và in lại đúng khuôn `[^n]`; ngược lại **xoá marker** (để lại số vô nghĩa là cho FE vẽ chip bấm nhầm nguồn). `nCites == 0` thì mọi marker biến mất |
| Gộp trùng | `[^2][^2]`, `[^2] [^2]` liền nhau → một `[^2]` (model hay lặp sau mỗi vế) |
| Xoá ID kỹ thuật | Bỏ các cụm `article[_ ]?id`, `bài viết id`, `mã bài viết`, `source[_ ]?id` kèm số (có/không có dấu `:`/`=`/`#`, trong hoặc ngoài ngoặc đơn); sau khi bỏ thì dọn ngoặc đơn rỗng `()` và khoảng trắng kép |

Vì sao xoá marker chứ không thay bằng `[?]`: answer phải còn đọc được khi in ra
giấy/không có FE; một ký tự lạ giữa câu hại hơn là mất dấu.

## Thay đổi

| File | Việc |
|---|---|
| `src/agent/usecase/citation.go` (mới) | `normalizeCitations` + các regex dựng một lần ở package level (compile tay để không trả error runtime), comment tiếng Việt nói rõ vì sao BE phải chốt lại dù đã có prompt |
| `src/agent/usecase/chat_service.go` | `Answer: normalizeCitations(res.Answer, len(res.Hits))`; log 1 dòng WARN khi có marker bị loại (không in answer, chỉ đếm số marker bị bỏ) để thấy model lệch bao nhiêu |
| `src/agent/usecase/citation_test.go` (mới) | table test: `[*2]` → `[^2]`, `[2]` → `[^2]`, số ngoài phạm vi bị xoá, `nCites=0` xoá hết, gộp trùng, "Article ID 54" bị bỏ và ngoặc đơn rỗng không còn sót, text thường có `[a](b)`/số trong câu không bị chạm, marker trong code fence vẫn bị chỉnh (chấp nhận — không cần né, ghi rõ trong comment) |

Không đụng FE (đợt 23 render `[^n]` thành chip bấm được), không đụng collector
số n, không đổi prompt.

## Kiểm chứng

- `go build ./...`, `go vet ./...`, `golangci-lint run ./...`, `go test ./...`
  (dáng hàm mới phải trong ngân sách 60 dòng/12 điểm của `src/quality`)
- `go test ./src/agent/usecase/ -run Citation -v`
- E2E thật: gọi `POST /api/v1/chat` với câu buộc phải trích dẫn trên dữ liệu thật
  (danh mục + tài liệu test), xem `answer` trong response: không còn `[*n]`, không
  còn "Article ID", số marker ≤ số cites trả về.
- `codegraph sync`.

## Kết quả

### Đã làm

| File | Việc |
|---|---|
| `src/agent/usecase/citation.go` (mới) | `normalizeCitations(answer string, nCites int) (string, int)` — khác chữ ký ghi trong kế hoạch ở chỗ trả thêm **số marker bị loại**, để caller log được mức model lệch bao nhiêu mà không in answer. Nhận diện theo **dãy** marker (`citationRun`) rồi mới tách số trong từng cái, vì RE2 của Go không có backreference nên không thể "thay và giữ lại group" để gộp trùng bằng một regex đơn |
| `src/agent/usecase/chat_service.go` | Dựng cites xong mới chuẩn hoá (`normalizeCitations(res.Answer, len(cites))`); WARN `chat: bỏ marker trích dẫn không có trong cites` kèm `trace_id`/`dropped_markers`/`cites`. Tách `buildCites(hits)` ra hàm riêng — `Chat` đội lên 63 dòng, vượt ngân sách 60 của `src/quality` (kế hoạch không nói trước chỗ này) |
| `src/agent/usecase/citation_test.go` (mới) | 16 case bảng + 1 case "không sót marker ngoài phạm vi/không đánh oan marker hợp lệ": khuôn `[*n]`/`[n]`/`[ ^ n ]`, số quá trần, `n=0`, `nCites=0`, gộp trùng liền và cách nhau 1 space, hai số khác nhau giữ thứ tự, ID kỹ thuật EN + kiểu Việt ("mã bài viết #12", "nguồn id = 3"), ID xen giữa hai marker, answer rỗng, `[tài liệu](link)`/`[ ]` checkbox không bị đụng, bảng markdown giữ nguyên canh lề khi không có gì bị cắt |

Một sửa đổi so với thiết kế ban đầu: cụm ID bị lộ được thay bằng **một dấu cách** chứ
không phải chuỗi rỗng. Regex `leakedArticleID` đã nuốt luôn khoảng trắng hai bên, thay
bằng `""` sẽ dán liền hai từ ("Áp dụng theovà.") — test bắt được ngay lần chạy đầu.
Tidy (ngoặc rỗng, space kép, space trước dấu câu) chỉ chạy khi thực sự có gì bị cắt,
nếu không sẽ phá canh lề code block/bảng.

### Kiểm chứng

- `go build ./...`, `go vet ./...` sạch; `golangci-lint run ./...` → `0 issues.`;
  `go test ./...` → 20 package `ok`, 0 FAIL (lần chạy đầu `TestFunctionBudget` đỏ ở
  `(*chatService).Chat` 63 dòng — xử lý bằng cách tách `buildCites`, không nâng trần).
- `go test ./src/agent/usecase/` → `ok`.

### E2E thật (`POST /api/v1/chat`)

User mới (id 93) → danh mục 129 → upload `dot22_fixture.txt` (quy định 3 Điều) qua
presign batch → PUT lên kho (http 200) → article 72 → bật `cmd/worker` để extract +
index (`extract=done index=done`) → hỏi câu buộc trả lời từ **hai Điều khác nhau**.

- Response: `question_type=lookup`, **4 cites** (`n=1..4`), answer:
  `… (Điều 1.2) … (Điều 1.3). [^3]` / `… (Điều 2.2). [^2]`, mất 47,8s.
- Quét toàn bộ answer: 2 token ngoặc vuông, **cả hai đúng khuôn `[^n]`**, `min=2
  max=3 < cites=4` → **0 marker ngoài phạm vi**; `Article ID`/`article_id`/`nguồn
  id`/link `http…` đều **False**.
- Log không có dòng `chat: bỏ marker…` → lượt này model viết đúng khuôn nên
  `dropped=0`. **Nhánh clamp vì thế chưa được đá bằng model thật** — phần đó chỉ
  có test unit khoá (nói thẳng ra để lần sau đừng tưởng đã chứng minh bằng runtime).
- Trở ngại môi trường (không liên quan code): model mặc định `9router` treo 300s trả
  `context deadline exceeded` (một lần khác 200 nhưng `empty choices`), `groq` tràn
  `ITPM 7000` vì system prompt + tool ~12k token; phải chuyển sang provider
  `openrouter` cho lượt E2E. Config E2E dùng bản copy ở TEMP có `timeout_sec` 900,
  `configs/config.local.json` không bị sửa.

### Dọn dẹp

`DELETE /articles/72` + `DELETE /categories/129` qua API (xoá mềm) — kỳ quét purge
của worker chạy sau đó thật: kiểm tra lại thấy `article_chunks` filter
`article_id=72` còn **0 point**, `article_pages` và row `articles`/`article_sections`
cũng 0 (janitor đã dọn blob → vector → row sau grace 5 phút). User 93 và category
129 soft-delete bằng `docker exec … psql` (không có route tự xoá tài khoản).
Xoá `ki.exe`, `ki_worker.exe`, `dot22_*.ps1`, fixture, body chat, config TEMP và
các log TEMP — riêng `dot22_worker_err.log` vẫn còn: worker đã chết nhưng handle
redirect của nó chưa được giải phóng nên Windows từ chối xoá; file nằm ngoài repo,
không ảnh hưởng gì, lần dọn sau xoá tiếp.
