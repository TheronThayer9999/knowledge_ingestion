# Đợt 5 — Tool báo trang PDF không đọc được (bỏ VLM khỏi pipeline)

## Quyết định

1. **VLM KHÔNG chạy tự động trong phase extract nữa.** Bỏ hướng gate + `max_pages_per_doc` ở §6.12-c.
   Chữ in (Việt/Anh) đã do OCR `vie+eng` lo (đợt 4), trang bảng do L1 `layout_score` → L2 tất định.
2. **Thêm tool cho agent** `list_unreadable_pages`: trả về *trang nào* của một file mà hệ thống không
   trích được chữ. Agent dùng thông tin đó để trả lời trung thực ("tài liệu thiếu phần ở trang N") hoặc
   (đợt 6) gọi tool đọc ảnh cho đúng trang đó.

## Vì sao cần tool này

Extract đã fail khi **cả file** không ra chữ, nhưng **file 100 trang mà 3 trang scan mờ** thì job vẫn
`done`: 3 trang đó có `text = ''` trong `article_pages` và **không ai biết**. Người dùng hỏi đúng phần
nằm ở trang 3 sẽ nhận câu trả lời thiếu mà không có cảnh báo — đây là kiểu sai nguy hiểm nhất của RAG
(im lặng, không phải trả lời sai ồn ào).

Chi phí gần bằng 0: 1 câu SQL trên `article_pages`, không thêm cột, không gọi model.

## Cách xác định "không đọc được"

`char_length(btrim(text)) < min_chars`, mặc định **40** ký tự (đổi được qua args, trần 1000).

- Dùng `char_length` chứ **không** `octet_length`: tiếng Việt 3 byte/ký tự, `octet_length` sẽ biến
  ngưỡng 40 thành ~13 ký tự thật.
- Trang ngắn THẬT (trang bìa phụ, trang ký, "Chương II" đứng riêng) cũng bị bắt — chấp nhận: tool chỉ
  **báo**, agent mới là bên quyết định nói gì với người dùng.
- Trang bảng vẫn có thể nằm trong danh sách (text rỗng) — `layout_score` trả kèm để phân biệt "trang
  bảng bị mất" với "trang trắng thật".

## Thay đổi

| Nơi | Việc |
|---|---|
| `domain/article_page.go` | `PageIssue` (page_num, seq, chars, layout_score) + vai hẹp `ArticlePageIssues` |
| `infrastructure/repository/article_page_repository.go` | `ListPageIssues(articleID, minChars)` — 1 câu SQL, không kéo `text` về |
| `agent/eino/list_unreadable_pages.go` | tool `list_unreadable_pages` (scope theo owner + category như `article_info`) |
| `agent/eino/tools.go` | `ToolDeps.Pages` + đăng ký tool |
| `loader/agent.go` | truyền `domain.ArticlePageIssues` vào `ToolDeps` |
| `loader/loader.go` | bind vai hẹp → `ArticlePageRepository` (thiếu dòng này boot fail "missing type") |
| `RAG_RESEARCH.md` §6.12 | VLM = tool agent, không phải bước ingest; bỏ `max_pages_per_doc` khỏi phase 0 |

## Hệ quả với agent

- Tool trả `page_count` + `min_chars` + danh sách trang ⇒ LLM nói được "N/M trang".
- Khi danh sách rỗng và `page_count > 0` ⇒ nội dung đã trích đủ, không có note gì thêm.
- `page_count = 0` ⇒ bài chưa trích xong; tool trả note "chưa kiểm tra được", không bịa là sạch.
- **Không** nhắc tới `read_page_visual` trong mô tả tool: tool đó chưa tồn tại, LLM sẽ gọi vào ngõ cụt.
  Đợt 6 mới thêm và sửa mô tả cùng lúc.

## Nghiệm thu

- [x] Test fake: thiếu scope → lỗi; `article_id` vô nghĩa → kết quả rỗng có note, không error; bài của
      user khác / ngoài category scope → rỗng (không lộ trang); `min_chars` mặc định + kẹp trần đúng.
- [x] `go build ./...`, `go vet ./...`, `golangci-lint run ./...`, `go test ./...` xanh.
- [x] Boot API: fx resolve được `domain.ArticlePageIssues` (không "missing type").

## Đợt 6 (dự kiến, sau khi spike Ollama)

**Trạng thái: HOÃN theo yêu cầu người dùng (25/10/2026) — "tính năng này để sau".** Không code trước khi
spike Ollama đo được chất lượng đọc ảnh thật; làm sớm chỉ tốn công đoán model.

`read_page_visual(article_id, page, mode)`: tải blob → render đúng trang (`pdftoppm`, cần export helper
trong `extractor`) → `IVLMReader.Transcribe/Ask`. Chỉ đăng ký tool khi `vlm.enabled`; ngân sách 1–2
lần/request; lỗi thì trả "không đọc được" chứ không làm hỏng run. Khi tool đó ra đời thì mới sửa `note`
của `list_unreadable_pages` để nhắc agent gọi nó.

## Commit

1. `docs(rag): add batch 5 plan for unreadable-page tool`
2. `feat(pages): query pages with little or no extracted text`
3. `feat(agent): add list_unreadable_pages tool`
4. `docs(rag): make VLM an agent tool instead of a pipeline step`

## Kết quả

- `golangci-lint run ./...` → 0 issues; `go test ./...` toàn repo xanh.
- Test fake (`list_unreadable_pages_test.go`): 8 test — guard scope, args vô nghĩa, cắt trần 50, kẹp
  `min_chars`, bài chưa trích xong (`page_count = 0`).
- Test DB thật (`article_page_repository_test.go`, skip khi không có Postgres): khoá đúng 2 cái bẫy của
  ngưỡng — `char_length` đếm KÝ TỰ (`"Ngắn"` = 4, không phải 5 byte) và `btrim` phải có tập ký tự
  (`" \n\t \r\n "` = 0 chứ không phải 8); trang xoá mềm không lọt vào kết quả.
- Chạy thật trên DB dev: bài 23 (`host-integration-server.pdf`, 5644 trang) → `issue_count = 75`, trang
  397 có `chars = 0`, nhiều trang 25 ký tự (chỉ còn dòng footer) — đúng loại "mất nội dung mà job vẫn
  `done`" mà đợt này giải quyết.
- Boot `cmd/apis` với `config.local.json`: `[Fx] RUNNING` — provider `domain.ArticlePageIssues` và tool
  resolve được (thiếu binding là fail ngay lúc boot, xem `loader.go`).
- Phát hiện khi làm: `repository.NewArticlePageRepository` trả INTERFACE lớn nên không bind được sang vai
  hẹp; phải thêm constructor riêng `NewArticlePageIssues(db)` cùng gói (thay vì type-assert runtime hoặc
  đụng 5 stub trong test service).
- Dev DB có 236/14417 trang trắng hoàn toàn sau khi trim đủ whitespace — nếu chỉ trim dấu cách
  (`btrim(text)`) thì những dòng chỉ có `\n` sẽ được đếm 2 ký tự và lọt qua ngưỡng 1–2.
