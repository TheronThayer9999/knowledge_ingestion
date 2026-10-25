# Đợt 7 — Reader là "tool" (port + registry) + 2 tool agent cho Excel, không dựng MCP server

## Vì sao

Đợt 6 đã trích được `.xlsx`/`.pptx` bằng stdlib, nhưng dispatch vẫn là **một switch trong
`ExtractRawPages`** cộng thêm một bảng `supportedExts` song song. Hệ quả:

- Thay stdlib bằng `excelize`/`unioffice`/`pdfcpu` = sửa thân hàm lõi của pipeline + tự canh lại
  bảng đuôi (đúng chỗ đã từng lệch với whitelist upload).
- Không có bề mặt nào để "nhấc" phần đọc tài liệu ra thành process/plugin riêng. WordCraft
  (`storytold/wordcraft`) là ví dụ tham khảo: mỗi định dạng là crate riêng (`wordcraft-docx`,
  `wordcraft-formats`), `wordcraft-mcp` (L5) bọc đúng bộ command đó thành tool — engine không
  biết tool tồn tại. Mình lấy phần "mỗi định dạng một port", **không** lấy phần MCP server.
- Agent chưa có cách nào trả lời "file Excel có sheet nào / sheet A có những hàng nào":
  `extractXlsx` dồn cả workbook thành `## Sheet: tên` + dòng pipe, `knowledge_search`/
  `read_article` chỉ trả các đoạn rời nên LLM phải đoán cấu trúc sheet.
- Muốn đọc thêm định dạng thì phải sửa `switch`, `supportedExts`, test `SupportedExtsDispatched`
  — ba chỗ cho một việc.

## Quyết định

Biến **định dạng tài liệu** thành một cổng (port) duy nhất, đăng ký trong một **registry hình
dạng MCP tool**:

```
ExtractRawPages (pipeline, không đổi chữ ký)
        │  ReadRequest{Key, Data, OCR, Layout, Have}
        ▼
Registry.Read ──theo đuôi──▶ Reader (1 định dạng = 1 tool)
                                 ├─ read_text   (.txt .md .csv .json)   → plainTextPieces
                                 ├─ read_docx   (.docx)                 → extractDocx
                                 ├─ read_xlsx   (.xlsx)                 → extractXlsx
                                 ├─ read_pptx   (.pptx)                 → extractPptx
                                 ├─ read_pdf    (.pdf)                  → extractPDFPages
                                 └─ read_image  (.jpg .jpeg .png .webp) → OCRImage
```

- **Không thêm dependency** (yêu cầu "un dependency"). `Reader` chỉ là interface; stdlib vẫn là
  implementation hiện tại.
- `ReaderInfo{Name, Description, Exts}` = descriptor kiểu MCP (`tools/list`), `Registry.Read` =
  `tools/call`. Hôm nay chạy in-process; nếu sau này cần khác process thì chỉ thêm adapter gọi
  `Registry.Read` ở đầu bên kia — pipeline không đổi.
- **Không dựng MCP server**: bản thân app đã là agent (Eino in-process), thêm process + transport
  chỉ đổi lấy đúng chức năng đó. Phần cần tách là ENGINE ĐỌC (nặng, dễ thay), và nó đã nằm sau
  interface `Reader`.
- Với agent, bề mặt "tool" là **Eino tool in-process**: `list_sheets` (file có sheet nào) và
  `read_sheet` (đọc dữ liệu 1 sheet theo hàng) — trả lời trực tiếp câu hỏi về Excel, không qua
  MCP.
- Registry được xây theo kiểu **"reader đăng ký sau thắng"** → thay engine của một định dạng là
  đổi đúng 1 dòng trong `defaultReaders()`, và test có thể `DefaultRegistry().With(fake)` mà
  không đụng bản mặc định (không mutable toàn cục).
- `Supports`/`SupportedExts` **suy ra từ registry** ⇒ hết khái niệm "bảng đuôi thứ hai"; xoá
  `supportedExts`, `errNoHandler` và `extractOffice`.

## Thay đổi

| Nơi | Việc |
|---|---|
| `src/common/extractor/reader.go` | `Reader`, `ReaderInfo`, `ReadRequest`, `ReaderFunc` (adapter cho hàm thường / thư viện), `Registry` (`NewRegistry`, `With`, `ReaderFor`, `Tool`, `Supports`, `Exts`, `Tools`, `Read`), `DefaultRegistry()`; doc block: cách cắm thư viện + bảng ánh xạ MCP |
| `src/common/extractor/readers.go` | 6 adapter mỏng + `defaultReaders()` — **chỗ duy nhất phải sửa khi đổi/thêm định dạng** |
| `src/common/extractor/extractor.go` | `ExtractRawPages` chỉ còn dựng `ReadRequest` rồi `DefaultRegistry().Read`; `Supports`/`SupportedExts` delegate; bỏ `supportedExts`, `errNoHandler`, switch |
| `src/common/extractor/office.go` | bỏ `extractOffice` (dispatch giờ thuộc registry) |
| `src/common/extractor/reader_test.go` | tool metadata hợp lệ (tên/mô tả/đuôi, không trùng), định tuyến theo đuôi, `With` ghi đè đúng 1 định dạng và không làm bẩn registry mặc định, đuôi lạ ⇒ `ErrUnsupported`, `ReaderFunc` dùng được |
| `src/common/extractor/office_test.go` | bỏ `TestSupportedExtsDispatched` cũ (giờ là test hình dạng registry) |
| `src/common/extractor/sheets.go` | `ParseSheetBlocks`: tách text `.xlsx` đã trích thành `SheetBlock{Index, Name, Header, Columns, Rows, Chars}` — đọc sheet từ `article_pages`, không mở lại blob |
| `src/domain/article_page.go` | vai hẹp `ArticlePageText` (chỉ `ListPagesRange`) cho tool agent — không mở đường ghi |
| `src/agent/eino/list_sheets.go` | tool `list_sheets`: tên sheet, số hàng, tên cột, độ dài; note khi chưa trích/không phải Excel/file quá lớn |
| `src/agent/eino/read_sheet.go` | tool `read_sheet`: chọn sheet theo số thứ tự hoặc tên, đọc theo hàng (`from_row`/`max_rows`), `next_from_row` để đọc tiếp |
| `src/agent/eino/sheet_scan.go` | đọc `article_pages` theo cửa sổ + tách sheet + kiểm tra quyền sở hữu dùng chung |
| `AGENTS.md` | 1 dòng ở mục ingestion: reader là port, thêm/thay định dạng chỉ sửa `readers.go`; 1 dòng về 2 tool sheet |
| `RAG_RESEARCH.md` §6.19 | ghi quyết định + ánh xạ MCP + ví dụ thay `excelize` |

## Tool agent cho Excel (`list_sheets`, `read_sheet`) — in-process, không MCP

Hai tool chỉ ĐỌC `article_pages` (text đã trích), không mở blob, không ghi gì:

```
list_sheets(article_id)          → [{index, name, rows, columns, chars}], sheet_count
read_sheet(article_id, sheet,    → {text: header + các hàng, total_rows,
           from_row, max_rows)     from_row, to_row, next_from_row, note}
```

- `sheet` nhận **số thứ tự** ("2") hoặc **tên** ("Bảng lương"); khớp chính xác thắng khớp chứa,
  khớp nhiều sheet thì trả note liệt kê (`1=Phí, 2=Phụ phí`) để LLM chọn lại — không tự đoán.
- Đọc theo **hàng** (không phải theo ký tự) nên `next_from_row` nối tiếp được; trần 300 hàng/lần
  chặn kiểu hỏi "cho tôi cả sheet" làm nổ context.
- File không phải Excel: 1 câu SELECT mảnh đầu là đủ để trả note "tài liệu này không có sheet" —
  không quét 5000 trang PDF.
- Quyền: `GetByID(user_id)` + `AllowsCategory` như `list_unreadable_pages`; bài ngoài phạm vi trả
  cùng câu với "không tìm thấy" để không lộ là bài tồn tại.
- Giới hạn đã biết: "header" là **hàng đầu tiên** của sheet (không đọc metadata Excel), và text
  đã trích chỉ có giá trị ô (công thức lấy kết quả đã lưu) — đúng với dữ liệu dùng để trả lời.

## Cách thay bằng thư viện (sau này, không làm bây giờ)

```go
// readers.go
func defaultReaders() []Reader {
	return []Reader{textReader{}, docxReader{}, excelizeReader{}, pptxReader{}, pdfReader{}, imageReader{}}
}

// xlsx_excelize.go — phần còn lại của hệ thống không đổi một dòng nào
type excelizeReader struct{}

func (excelizeReader) Info() ReaderInfo {
	return ReaderInfo{Name: "read_xlsx", Description: "Trích Excel bằng excelize", Exts: []string{".xlsx"}}
}

func (excelizeReader) Read(ctx context.Context, req ReadRequest, visit func(Page) error) error {
	f, err := excelize.OpenReader(req.Data) // cần seek → đọc archive vào RAM như hiện tại
	...
	return visitPieces(0, text, visit)
}
```

## Nếu sau này mới tách process (bề mặt đã sẵn hình dạng MCP)

| Hôm nay (in-process) | Tương đương MCP |
|---|---|
| `Registry.Tools()` | `tools/list` (name, description, `exts` trong inputSchema) |
| `Registry.Read(ReadRequest{...}, visit)` | `tools/call` với `{path|base64, ext}` |
| reader đẩy `Page` qua `visit` | kết quả tool: `[{num, seq, text}]` |

Worker vẫn gọi `ExtractRawPages`; khi tách, reader trở thành `ReaderFunc` gọi client MCP —
registry không đổi. Điều kiện để tách được là **`ReadRequest` không chứa thứ gì thuộc queue/DB**;
đó là lý do nó chỉ có `Key/Data/OCR/Layout/Have`.

## Không làm

- Không thêm thư viện nào (giữ "un dependency"); `excelize` chỉ xuất hiện trong doc làm ví dụ.
- Không dựng MCP server thật (quyết định của người dùng: tool in-process cho gọn), không đổi chữ
  ký `ExtractRawPages` (25+ call site).
- Không đụng `NeedsDespace`, chunker, `article_pages` — cổng này dừng ở text thô.
- Không parse công thức/định dạng ô/ảnh trong Excel — chỉ đọc giá trị đã lưu như phase extract.

## Nghiệm thu

- [x] `go test ./src/common/extractor/ ./src/controller/services/` xanh, gồm: 3 tool Office tách
      riêng (`read_docx/read_xlsx/read_pptx`), `With(excelize giả)` đọc `.xlsx` bằng reader mới
      còn `.docx` vẫn về stdlib, đuôi lạ ⇒ `ErrUnsupported`.
- [x] `SupportedExts()` vẫn đủ 12 đuôi và khớp `file_service.allowedExts` (test cũ xanh).
- [x] `list_sheets` trả đúng tên sheet/số hàng/cột từ `article_pages`; file không phải Excel và
      bài chưa trích đều có note riêng (không bịa sheet).
- [x] `read_sheet` phân trang theo hàng (`next_from_row` nối tiếp), chọn theo số thứ tự hoặc tên,
      chặn scope như `list_unreadable_pages` (test `sheet_tools_test.go`).
- [x] `go build ./...`, `go vet ./...`, `golangci-lint run ./...`, `go test ./...` xanh; FE không
      đổi file nào nên không cần chạy lại `npm run lint`.

## Commit

1. `docs(extractor): add reader port plan`
2. `refactor(extractor): turn format extraction into pluggable readers`
3. `feat(agent): add list_sheets and read_sheet tools for excel`
4. `docs(rag): record reader port and excel sheet tools`

## Kết quả (25/10/2026)

- `reader.go` / `readers.go`: 6 reader, registry "sau thắng", `With()` trả bản sao;
  `ReaderFor` nhận cả `.xlsx`, `XLSX` và `uploads/a.XLSX`.
- `ExtractRawPages` chỉ còn dựng `ReadRequest`; xoá `supportedExts`, `errNoHandler`,
  `extractOffice`; `Supports`/`SupportedExts` suy ra từ registry.
- `reader_test.go` (6 test) + `sheets_test.go` (4 test) + `sheet_tools_test.go` (11 test).
- Verify: `go build`, `go vet`, `golangci-lint run` (0 issues), `go test ./...` toàn bộ xanh.

