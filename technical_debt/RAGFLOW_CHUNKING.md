# RAGFlow xử lý chunk thế nào — nghiên cứu & áp dụng

Nguồn: https://github.com/infiniflow/ragflow (bản Go, nhánh `main`, đọc tháng 10/2026).
License RAGFlow: Apache-2.0. Chỉ học ý tưởng + tham chiếu file, không copy code.

## 1. Parse DOCX: giữ block có type, không flatten thành text thô

- `internal/deepdoc/parser/office/parser.go` + `types.go`: DOCX → `office_oxide`
  (Rust core qua Go binding) → IR JSON → `[]RawBlock` theo đúng thứ tự văn bản.
- Mỗi block có type riêng:
  - `paragraph` kèm `Style` (vd `"Heading 1"`) — heading nhận diện bằng **style thật
    của Word**, không regex đoán;
  - `table` giữ nguyên `Rows [][]string`;
  - `image` giữ bytes base64.
- `blockToSection` gắn 2 tag: `DocTypeKwd` (`text`/`table`/`image`) và `LayoutType`
  (`title` cho heading). Bảng convert sang HTML (`SimpleRowsToHTML`), giữ thêm
  `TableItem` structured.

=> Bài học: extractor nên xuất **typed blocks** (paragraph + style/level, table rows,
image caption) ngay từ phase 1, đừng flatten thành 1 string để phase 2 đoán lại
bằng regex. `office_oxide` nặng (Rust + CGO) — mình giữ stdlib, chỉ học cấu trúc
output của nó.

## 2. Bảng: chunk độc lập + prefix vị trí + context xung quanh

- Mỗi bảng = **1 chunk riêng**, kèm `<caption>Table Location: {title}</caption>` —
  title là chuỗi phân cấp tìm ngược lên trên (`tên_doc > Heading 1 > Heading 2`,
  hàm `__get_nearest_title` trong `rag/app/naive.py` bản Python cũ).
- Tham số `table_context_size`: lấy thêm N token văn bản xung quanh (trên/dưới)
  fold thẳng vào body chunk (`materializeMediaContext` trong
  `internal/ingestion/component/chunker/common.go`).
- Bảng quá to: `html_rows.go` — cắt theo hàng, lặp lại header.

## 3. Ảnh: chunk riêng type `image`, không drop

- Ảnh thành chunk `DocTypeKwd: "image"` mang theo image data + caption/context
  (`image_context_size`, `image_upload.go` lưu crop lên MinIO).
- PDF scan qua OCR/layout (`DeepDoc`, vision enhancement).

=> Quy ước hiện tại của mình (giữ caption, bỏ ảnh trần) là bản lightweight đúng
hướng. OCR để ngoài scope.

## 4. Điều khoản: HierarchyTitleChunker — cây heading + DFS

`internal/ingestion/component/chunker/hierarchy.go`:

- Dựng **cây heading** bằng stack (heading level cao làm cha), mỗi node giữ
  `titleIndexes` + `bodyIndexes` + children.
- Duyệt **DFS**, mỗi chunk = **đường dẫn tiêu đề từ gốc tới node + body của node**
  (`path_titles` làm tiền tố). Mọi chunk mang full ngữ cảnh phân cấp
  (`Hợp đồng > Chương 2 > Điều 5`), không chỉ 1 cấp.
- Gặp block phi-text (bảng/ảnh): flush run text đang gom, block đó thành group
  riêng — bảng trong Điều 5 không bị trộn vào text điều khoản.
- Xong mới áp `chunk_token_cap` (đo bằng tokenizer thật).

Thứ tự pipeline khuyến nghị (docs `configure_chunker_component`):
**Parser → Token Chunker → Title Chunker**. Nối Title trực tiếp vào Parser gây lỗi
format với email/ảnh/spreadsheet. Tách 2 tầng = đúng 2 phase của mình.

## 5. Hai chi tiết củng cố thiết kế sẵn có

- **Chunk ID deterministic**: `canonicalChunkID = hash(docID, canonicalText)` —
  hash trên text đã fold context.
- Quyết định của project: dùng **UUIDv7 sinh 1 lần lúc chunk, lưu cột `point_id`**,
  phase 2 đọc lại — retry an toàn tương đương (upsert đúng point cũ), lại sort
  được theo thời gian và không phụ thuộc nội dung text.
- **Delimiter lossless**: chế độ keep-delim giữ dấu cắt dính vào chunk
  (`"a。b。"` → `["a。", "b。"]`), không xóa ký tự. `mergeOverlap` của mình dùng
  `strings.Join(cur, " ")` làm mất dấu `\n\n` gốc — nên join bằng delimiter gốc.

## 6. Áp vào project (theo thứ tự)

1. **Extractor xuất typed blocks** (paragraph + style/level, table rows, image
   caption) thay vì 1 string. Phase 1 vẫn lưu `Content` như cũ; thêm cột
   `chunk_type` + `heading_path` vào `article_chunks` sau. Extractor nên trả
   struct từ đầu để khỏi sửa lại.
2. **Chunk bảng**: 1 bảng = 1 chunk HTML/pipe + prefix
   `Table Location: <heading path>` + context window trên/dưới; bảng to cắt
   theo hàng, lặp header.
3. **Chunk điều khoản**: dựng cây theo Word Heading style (có sẵn trong docx,
   khỏi regex `Điều X`); DFS emit chunk kèm full path. Regex chỉ là fallback cho
   file không dùng style.
4. **Join delimiter lossless** trong `mergeOverlap` (`src/common/chunker/chunker.go`):
   join bằng delimiter gốc thay vì `" "`. Sửa nhỏ, test cũ vẫn pass.
5. **Đo size bằng token** khi có embedding pipeline (phase 2); tạm thời đếm rune
   vẫn ổn.
