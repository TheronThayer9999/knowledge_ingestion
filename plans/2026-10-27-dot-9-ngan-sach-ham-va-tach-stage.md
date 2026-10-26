# Đợt 9 — Ngân sách hàm cho pipeline (hàm không quá to, không vụn)

## Bối cảnh: đo trước, không đoán

Quét toàn bộ 425 hàm non-test dưới `src/` (không tính `_test.go`):

| Chỉ số | Giá trị |
|---|---|
| trung vị | 12 dòng |
| p90 | 42 dòng |
| > 60 dòng | 18 hàm |
| > 80 dòng | 11 hàm |
| dài nhất | `indexOne` **178**, `extractDocx` 166, `extractOne` 142, `xlsxRows` 112 |
| ≤ 4 dòng | 64 — hầu hết là constructor repository, hàm thoả interface, hoặc predicate có tên |

Pipeline **giữa các stage đã đúng** (cột queue → extract service → `article_pages` → index
service → checkpoint → compile): mỗi stage đọc một cửa sổ, làm một việc, resume được. Vấn đề nằm
**trong** 2 hàm, không phải giữa các stage. Vì vậy đợt này không đụng kiến trúc, chỉ đặt ngân sách
và tách đúng 2 hàm to nhất (đã có test bao).

## Ngân sách (chốt, dùng cho cả test và review)

| Quy tắc | Ngưỡng | Lý do |
|---|---|---|
| Kích thước đích | 8–45 dòng | vừa một màn hình, vừa một tầng `if` để nhớ |
| Trần cứng | 60 dòng | > 60 gần như chắc chắn trộn ≥ 2 tầng trừu tượng |
| Điểm quyết định (`if/for/case/&&/\|\|`) | ≤ 12 | `indexOne` 29 điểm là lý do nó khó sửa |
| Vòng lặp lồng | ≤ 2 | 3 vòng = người đọc phải mô phỏng state 3 chiều |
| Một tầng trừu tượng / hàm | orchestrator gọi stage, stage không gọi nhau | |
| State giữa các bước | struct có tên, **không** closure | `flush` trong `indexOne` đóng kín 4 biến — người đọc không thấy, test không chạm tới |

**Tách khi**: comment bắt đầu bằng "Bước N"; một khối cần ≥ 4 biến cục bộ chỉ dùng trong khối;
hai khối dùng chung đúng 3 biến; thân vòng lặp có thể test riêng được.

**KHÔNG tách (vụn)**: hàm nhỏ là đúng khi nó *chính là* khái niệm — (a) thoả interface/constructor,
(b) tên miền dùng trong hội thoại (`IsSpreadsheetQuestion`), (c) có ≥ 2 caller hoặc có test. Chỉ
cấm **pass-through**: thân đúng 1 lệnh chuyển tiếp, 1 caller, không thêm hành vi (panic guard,
detach ctx, log là hành vi → được). Không đặt "trần dưới" bằng số dòng: cùng 3 dòng nhưng "khái
niệm" khác "chuyển tiếp" ở ngữ nghĩa, máy không phân biệt được → **không** đưa thành test, chỉ
ghi vào review checklist (AGENTS.md).

## Phần A — Test chặn hồi quy (`src/quality/`)

`src/quality/doc.go` (chỉ package + comment, để `go build ./...` không báo
"no non-test Go files") và `src/quality/function_budget_test.go`:

- Duyệt `../..`, bỏ qua `.git`, `knowledge_ingestion_fe`, `node_modules`, `docs`, `vendor`,
  `_test.go`.
- `go/parser` (stdlib) → mỗi `FuncDecl`: đếm dòng = `End().Line - Pos().Line + 1`; đếm điểm
  quyết định = `IfStmt/ForStmt/RangeStmt/CaseClause/CommClause` + `BinaryExpr{LAND,LOR}`.
- Vượt ngân sách mà không có trong allowlist → fail, in `file:line tên (dòng, cx)`.
- Allowlist là **slice đã sắp xếp** `[]allowEntry{{Key, Lines, CX}}` (không phải map: map không có thứ tự nên test "allowlist phải sorted" không viết được); `allowlistByKey()` dựng map tra cứu mỗi lần chạy.
- Allowlist ghi **đúng số đã đo**; vượt số đã ghi → fail (hàm phình
  thêm); hàm đã hết vi phạm mà vẫn còn entry → fail (bắt xoá entry). Danh sách chỉ ngắn đi.
- Key = `đường/dẫn/file.go::TênHàm` (method: `(*Type).Method`) → đổi tên hàm là mất entry, test
  báo ngay.

## Phần B — `indexOne` 178 → ~40 dòng + stage

Giữ nguyên file `article_index_service.go` (`pageWindow`/`piece`/`cuts` không phải chuyển đi —
thứ cần sửa là kích thước hàm, không phải vị trí file); thêm `eachPageWindow` + `indexRun`
và các method stage của nó. Tên cuối cùng: `newIndexRun`, `prepare`, `dropStaleVectors`,
`indexWindow` (thay `fillWindow`), `consumeWindow`, `appendPiece`, `addChunks`, `flush`, `finish`.

```
indexOne
├─ buildCleaner            (giữ, pass 1)
├─ dropStalePoints         (khối resumeFrom == 0)
├─ newIndexSession         (batch, total, resume, carry, splitter, start)
├─ eachPageWindow(...)     (vòng đọc trang — DÙNG CHUNG với buildCleaner)
│   └─ fillWindow → indexWindow
│       ├─ piecesOf        (win.cuts + bỏ mảnh rỗng)
│       ├─ splitPiece      (Split + Annotate + carry)
│       └─ addChunks       (skip resume, batch, flush khi đầy, ensureAlive)
├─ flush
└─ guard "0 chunk" + log
```

Sửa kèm 3 lỗi thật phát hiện khi đọc:

1. `chunker.NewTemplate` + `DetectType` đang dựng **trong vòng lặp trong cùng** (mỗi *mảnh* một
   lần, `article_index_service.go:327-336`) → nâng lên 1 lần/bài (`template(article)`), xoá nhánh
   `if article.DocType == ""` lặp lại.
2. Vòng "đọc trang theo cửa sổ tới khi hết" viết **hai lần** trong cùng file (`:296`, `:416`) →
   một `eachPageWindow`; điều kiện dừng duy nhất (`len(pages) == 0`) nằm đúng một chỗ — chính là
   thứ `TestIndexOneReadsInWindows` canh.
3. Comment của `lookupCache` (hàm đã xoá cùng embed_cache) còn nằm trên `ensureAlive` → xoá.

## Phần C — `extractOne` 142 → ~35 dòng + stage

```
extractOne
├─ loadSaved           (ListByArticle + seen + log "tiếp tục từ trang đã trích")
├─ extractRun.extract  (ExtractRawPages + visit — thay 3 biến closure bằng field)
├─ flushTail           (chỉ ghi ĐUÔI MỚI: fresh[len-sinceFlush:])
├─ guard "không trang nào ra chữ"
└─ finish              (pageCount, meta phiên bản/hiệu lực, nhánh bài bị xoá, UpdateMetadata)
```

- `rc` vẫn đóng ngay sau `extract` trong `extractOne` (kèm `defer` chỉ để chặn rò khi panic) —
  stage nhận `io.ReadCloser` sẵn nên không tự đóng, tránh 2 chủ sở hữu connection.
- `extractRun.visit` / `flushTail` là method → test được bằng stub, không phải chạy OCR thật.
- Comment của `extractOne` (dòng 172-183) đang nằm trên `releaseClaims` do lần chèn trước →
  trả về đúng hàm.

## Test

- `src/quality/function_budget_test.go`: bản thân nó là test; chạy trên repo thật (allowlist đã
  seed từ số đo ở trên).
- `src/controller/services/article_index_service_test.go` thêm `TestIndexWindowStopsAtCharCap`:
  cửa sổ dừng khi chạm `INDEX_WINDOW_MAX_CHARS`, `next` = seq trang kế **chưa** dùng, không trang
  nào bị bỏ qua hay đọc hai lần.
- `src/controller/services/article_extract_service_test.go` (mới) `TestExtractRunWritesOnlyNewPages`:
  `visit` bỏ qua seq đã có, `flushTail` chỉ ghi đuôi mới, gọi đúng số lô.
- Giữ nguyên 7 test hiện có của index làm lưới an toàn cho Phần B (`ChunkIndexAndPage`, `Resume`,
  `RejectsEmptyPages`, `MarksErrorWhenArticleDeleted`, `ReadsInWindows`).

## Verify

`go build ./...`, `go vet ./...`, `golangci-lint run ./...`, `go test ./...` → sau đó đo lại bằng
cùng script để chứng minh `indexOne`/`extractOne` đã rời top; `codegraph sync`.

## Kết quả

| Hàm | Trước | Sau (chỉ thân hàm, không tính doc comment) |
|---|---|---|
| `indexOne` | 178 dòng, ~29 điểm | **17 dòng**, 4 điểm |
| `extractOne` | 142 dòng, ~21 điểm | **29 dòng**, 5 điểm |

- Điểm quyết định đếm đúng như test (`If/For/Range/Case/CommClause` + `&&`/`||`, tính cả trong closure),
  kích thước là `End().Line - Pos().Line + 1` — doc comment **không** tính. Con số "29 dòng" ghi ở
  commit `refactor(index)` là do script đo tạm neo vào dòng trống trong doc comment; số đúng là 17.
- Allowlist còn **34 entry** (seed 36 → xoá 2 entry của 2 hàm vừa tách); test xanh nghĩa là
  allowlist khớp **chính xác** tập hàm vượt ngân sách hiện tại, không thừa không thiếu — nên 34
  chính là số hàm còn vượt trần (> 60 dòng hoặc > 12 điểm), không phải 36.
- Bug phát hiện khi làm: test ngân sách ban đầu **xanh rỗng** vì `root = "../.."` làm `d.Name() == ".."`
  khớp luật bỏ qua thư mục ẩn → `fs.SkipDir` ngay tại gốc; sửa bằng `path != root &&`.
- Ngoài kế hoạch: `chunker.NewTemplate`/`DetectType` vốn dựng **mỗi mảnh** trong vòng lặp trong
  cùng → nay 1 lần/bài (`resolveTemplate`).

## Commit dự kiến

1. `docs(plans): add batch 9 plan for function budget and pipeline stages`
2. `test(quality): guard function size and complexity with a shrinking allowlist`
3. `refactor(index): split indexOne into a page window session`
4. `refactor(extract): split extractOne into a page sink`
5. `docs(agents): record the function budget and naming rules`

## Nghiệm thu

- [x] `indexOne` ≤ 45 dòng (17), `extractOne` ≤ 45 dòng (29); không còn hàm nào > 80 dòng ngoài allowlist.
- [x] `chunker.NewTemplate` chỉ dựng 1 lần/bài (`resolveTemplate`); vòng đọc trang chỉ còn 1 bản (`eachPageWindow`).
- [x] Test mới khoá cửa sổ theo trần ký tự (`TestIndexWindowStopsAtCharCap`) và `extractRun` chỉ ghi đuôi mới (`TestExtractRunWritesOnlyNewPages`).
- [x] build/vet/lint/test sạch (`golangci-lint run ./...` → 0 issues); allowlist đã xoá 2 entry của 2 hàm vừa tách.
