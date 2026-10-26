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
