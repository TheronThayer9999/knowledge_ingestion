# Chunk tiếng Việt bằng Eino splitter — kế hoạch

Nguồn: https://github.com/cloudwego/eino-ext (`components/document/transformer/splitter/`,
`components/embedding/ollama`), đọc tháng 10/2026. License: Apache-2.0.

## 1. Vấn đề của code chunk thủ công hiện tại với tiếng Việt

- **Đếm byte thay vì đếm ký tự.** Chữ Việt có dấu chiếm 2–3 byte UTF-8, nên mọi
  ngưỡng `ChunkSize` tính bằng `len()` đều cho chunk nhỏ hơn 2–3 lần ý định.
- **Thiếu separators tiếng Việt.** Văn bản Việt dùng nhiều `…`, `...`, `;`, `:`
  mà splitter mặc định (`\n . ? !`) không có → ranh giới chunk xấu.
- **Viết tắt hành chính gây cắt nhầm câu** (`Tp.`, `TS.`, `PGS.`, `Q.`, `P.`):
  splitter ký tự thấy `.` là cắt, nát câu. Chỉ splitter ngữ nghĩa mới xử lý được.

## 2. Phương án: 2 tầng, giữ embedding + Qdrant hiện tại

Eino `Indexer`/`Retriever` chính thức chỉ có Elasticsearch và VikingDB, **chưa có
Qdrant**. Nên không thay cả pipeline — chỉ thay **phase split** bằng
`document.Transformer` của Eino, giữ nguyên code Ollama bge-m3 + Qdrant của repo.

- **Tầng 1 — bulk ingestion (worker chạy hàng loạt): `recursive` splitter.**
  Tương đương LangChain `RecursiveCharacterTextSplitter`, rẻ/nhanh, không tốn
  thêm call embedding. Đã triển khai (`chunker.NewRecursive`, tái dùng `Option`
  hiện tại). Config tiếng Việt (zero value là default này):
  ```go
  chunker.NewRecursive(chunker.Option{}) // separators VN + đo rune, xem strategy.go
  ```
  Separators: `{"\n\n", "\n", "…", "...", ".", "?", "!", ";", ":", " ", ""}`
  (`" "`/`""` cuối là lưới an toàn cho đoạn dài không dấu câu — Eino không có
  là trả nguyên đoạn quá cỡ). Semantic dùng riêng separators cấp câu
  (`sentenceSeparators`, không `" "`/`""`) kẻo tách vụn tới ký tự ngay từ đầu.
- **Tầng 2 — tài liệu quan trọng / văn bản hành chính: `semantic` splitter.**
  Cắt theo độ tương đồng embedding (cosine + ngưỡng `Percentile`), ranh giới do
  ngữ nghĩa quyết định nên miễn nhiễm viết tắt. Embedder là **adapter bọc
  client Ollama sẵn có của repo** (`embedderAdapter` trong `strategy.go`,
  float32→float64) — không thêm client thứ hai, giữ nguyên config/timeout hiện
  tại (bge-m3 đa ngữ 100+ ngôn ngữ, gồm tiếng Việt). Đã triển khai
  (`chunker.NewSemantic` + test cụm nghĩa với fake embedder, không gọi mạng):
  ```go
  chunker.NewSemantic(embedder, chunker.SemanticOption{MinChunk: 100})
  ```
  Chi phí: mỗi câu tốn 1 call embedding (+buffer) — chỉ dùng cho tài liệu ưu tiên.

Không dùng `markdown`/`html` splitter làm mặc định (chỉ hợp tài liệu có cấu trúc
sẵn); không nối recursive → semantic 2 tầng trừ khi retrieval hiện tại quá kém.

## 3. Tích hợp vào worker

1. Bọc splitter Eino sau extractor hiện tại: `text thô → []schema.Document`
   (1 doc/input, metadata giữ `source URI`, `chunk index` theo quy ước `MetaData`
   của Eino — transformer merge chứ không thay metadata).
2. Map `schema.Document` → chunk model của repo → embedding bge-m3 → Qdrant
   (code hiện tại giữ nguyên, chỉ đổi đầu vào từ chunk thủ công sang chunk Eino).
3. Chọn tầng 1 hay 2 theo flag/config ingestion (mặc định tầng 1).
4. Mỗi `splitter` là `go.mod` module riêng trong eino-ext (`go get
   github.com/cloudwego/eino-ext/components/document/transformer/splitter/recursive`),
   `go mod tidy` sau khi thêm.

## 4. Verify

`go build ./...` && `go vet ./...` && `golangci-lint run ./...`; test so sánh
retrieval trước/sau trên 1 văn bản hành chính mẫu nhiều viết tắt (đánh giá thủ
công top-k, chưa có harness tự động).

## 5. Retrieval hybrid — ĐÃ TRIỂN KHAI (semantic + full-text, không semantic-only)

- Hiện trạng cũ: `search_service` pure dense vector — semantic bỏ sót từ khóa
  chính xác (số hiệu văn bản, tên riêng, mã điều luật), nhất là tiếng Việt.
- Đã làm:
  1. Full-text index trên payload `text` ở `ensureCollection` (best-effort như
     3 index số; tokenizer Word + AsciiFolding để query không dấu vẫn trúng,
     lowercase mặc định) — `src/infrastructure/qdrant/connection.go`.
  2. `IVectorStore.SearchText` (MatchText trong cùng filter quyền
     `permissionMusts` dùng chung với dense — 2 nửa không lệch phạm vi) —
     `src/domain/vector_store.go`.
  3. `HybridSearcher` (`src/controller/services/hybrid_search.go`) tách method
     để agent tool tái dùng từng bước: `EmbedQuery` / `DenseSearch` /
     `TextSearch` (lỗi → warn + degraded dense-only) / `FuseRRF` (pure, k=60)
     / `FilterDone` (chặn bài chưa done) / `SearchHybrid` (pipeline đầy đủ).
     HTTP `Search` giờ chỉ map DTO → `HybridParams` rồi gọi pipeline.
- Instruction LLM (system prompt + tool `knowledge_search` + rule ưu tiên tài
  liệu upload) ghi ở `technical_debt/AGENT_LLM_INSTRUCTIONS.md` — port sang
  Eino agent sau (tool bọc `SearchHybrid`, test roundtrip đã liệt kê).
- Lưu ý trung thực: MatchText của Qdrant là boolean match, ranking chính đến
  từ RRF fusion — không phải BM25 scoring thật. Muốn BM25 thật thì đi sparse
  vectors (cần sparse model, nặng) — để sau nếu hybrid RRF chưa đủ.
