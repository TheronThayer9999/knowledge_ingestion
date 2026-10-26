# Đợt 7b — Để LLM thực sự gọi list_sheets/read_sheet khi cần dữ liệu bảng

## Vấn đề

Đợt 7 đã có 2 tool `list_sheets`/`read_sheet` và chúng nằm trong tool list của mọi
provider agent (`NewDefaultTools` → `BuildProviderAgents` → `adk.ToolsConfig`). Nhưng
"có trong tool list" ≠ "LLM dùng". Còn 3 lỗ hổng phía prompt:

1. `prompts/chat_system.md` mục 0 bước 1 chỉ liệt kê các đường đi quen: tìm kiếm,
   thông tin tài liệu, liệt kê mục, đọc theo trang, đọc thêm context — **không có
   đường "bảng Excel"**. Gặp câu "bảng lương có gì", model đi thẳng vào
   `knowledge_search`, nhận lại các mảnh pipe vỡ từ chunker rồi trả lời từ đó — đúng
   kiểu sai mà đợt 7 muốn chặn.
2. Trần "không quá 8 lượt gọi tool" chỉ miễn trừ "đọc theo trang để tóm tắt toàn bộ
   file". Đọc một sheet 600 hàng cần nhiều lượt `read_sheet` → model dừng giữa
   chừng rồi kết luận "bảng chỉ có chừng này".
3. Mục 11 ("Đếm và thống kê") không nói số hàng/số sheet lấy từ đâu, nên model dễ tự
   đếm bằng mắt trên vài dòng trả về.

## Quyết định

- **Không thêm tool, không sửa code tool**: tên/args/mô tả đã đủ (mô tả đã tự mô tả
  KHI NÀO GỌI / KHI NÀO KHÔNG / CÁCH DÙNG và trỏ qua lại giữa 2 tool).
- Sửa **prompt** để mở đúng đường đi: bước 1d nhận diện "bảng/sheet", mục "Thông tin
  tối thiểu" thêm mục dữ liệu bảng, trần lượt gọi nới cho đọc sheet, mục 7 thêm loại
  yêu cầu, mục 11 thêm 3 quy tắc gọi (liệt kê sheet trước → đọc theo hàng → đọc tiếp
  khi còn hàng; không trả lời bảng từ đoạn trích vỡ; số hàng lấy từ tool).
- Giữ nguyên văn phong prompt: mô tả công cụ theo **chức năng** ("công cụ liệt kê
  sheet", "công cụ đọc sheet theo hàng"), không chép tên tool — mục 11 đã chốt "danh
  sách tham số nằm ở định nghĩa tool đi kèm, nguồn thật duy nhất".
- Thêm test khoá phần "LLM nhìn thấy gì": `Info()` của 2 tool phải có name/desc khác
  rỗng, desc nhắc tool còn lại (để model biết phải đi theo cặp) và schema args phải
  khai `article_id`/`sheet`/`from_row`/`max_rows` kèm mô tả.

## Thay đổi

| File | Việc |
|---|---|
| `prompts/chat_system.md` | bước 1d; "Thông tin tối thiểu" thêm mục bảng; bước 2 nới trần; mục 7 thêm loại câu hỏi Excel; mục 11 thêm 3 gạch đầu dòng + 1 gạch ở "Đếm và thống kê" |
| `src/agent/eino/tool_metadata_test.go` | test mới: metadata + schema args của `list_sheets`/`read_sheet`, và `NewDefaultTools` trả đủ 9 tool không trùng tên |

## Nghiệm thu

- [x] `chat_system.md` có đường đi rõ cho câu hỏi bảng Excel ở cả 4 chỗ (kế hoạch,
      thông tin tối thiểu, loại yêu cầu, khi nào gọi công cụ).
- [x] `go test ./src/agent/eino/` xanh, gồm test metadata mới.
- [x] `go build ./...`, `go vet ./...`, `golangci-lint run ./...`, `go test ./...` xanh.

## Kết quả (phát sinh lớn hơn dự kiến)

Test metadata bắt được lỗi thật: eino tách tag `jsonschema` theo **dấu phẩy**, nên 6 tag có dấu
phẩy trong `description=` bị **cắt im lặng** — model đọc `Số hàng mỗi lần đọc (mặc định 60`,
`ID bài viết Excel (lấy từ knowledge_search`, `Hàng dữ liệu bắt đầu`, `Mức mặc định 1`... Đã sửa
sang `;` ở `read_sheet.go`, `read_article.go`, `read_context.go`, `list_unreadable_pages.go`.
Thêm test `TestToolArgTagsHaveNoCommaInDescription` soi toàn bộ struct args để lần sau không tái diễn
(và đã thử: escape `\,` không cứu được vì `reflect.StructTag.Get` unquote hỏng trước).

Ngoài ra mỗi desc tool sheet có thêm khối `VÍ DỤ` (gọi thế nào + đọc tiếp thế nào) vì few-shot
trong Desc là cách rẻ nhất tăng độ chính xác tool-calling.

## Commit

1. `docs(agent): add batch 7b plan`
2. `docs(prompt): teach the llm the excel sheet tools` (prompt route + desc VÍ DỤ + sửa 6 tag bị cắt)
3. `test(agent): lock the tool metadata the llm sees`
4. `docs(rag): record how the llm discovers sheet tools`
