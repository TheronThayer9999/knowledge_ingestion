# Plan tiếp theo — Knowledge Ingestion

Tổng hợp từ `IMPROVEMENTS.md`, đã hoàn thành P0 + P1.5–P1.8. Plan này chốt thứ tự và phạm vi cho các mục còn lại, theo mức ảnh hưởng retrieval.

## Phase A — Chất lượng retrieval (P1.2 → P1.3 → P1.4)

### A1. Template-based chunking (P1.2) — Ưu tiên cao
- **Mục tiêu:** thay 1 splitter cho mọi file bằng chiến lược theo loại tài liệu.
- **Phạm vi:**
  - Thêm `DocumentType` enum (General, Laws, Table, Paper, Book, Q&A, Manual) trong `src/common/chunker`.
  - Mỗi template là một `Chunker` riêng: Laws cắt theo Điều/Khoản, Table theo hàng, Paper theo section.
  - `Article`/`Category` lưu `doc_type` (migrate cột) hoặc đoán từ content_type + tên file.
  - Worker index chọn splitter theo `doc_type`, fallback recursive.
- **Rủi ro:** thay đổi chunk size → phải rebuild vector (dùng rebuild-vectors admin endpoint đã có).
- **Kế hoạch test:** unit test từng template với fixture văn bản pháp luật/bảng; regression test `TestSplitRespectsChunkSize`.

### A2. Dual embedding (P1.3)
- **Mục tiêu:** vector title + vector content kết hợp theo weight (`filename_embd_weight`).
- **Phạm vi:**
  - Qdrant payload thêm `title_vector`, `content_vector`.
  - Embedder gọi 2 lần (title = tên bài + header mục; content = chunk text).
  - Search query kết hợp 2 vector (weighted sum hoặc RRF).
- **Phụ thuộc:** A1 (chunk header phải ổn định).

### A3. Content enhancement (P1.4)
- **Mục tiêu:** LLM sinh keywords/questions/tags lưu trên chunk → retrieval tốt hơn.
- **Phạm vi:**
  - Service enhancement gọi LLM (dùng `llms` config) cho mỗi chunk, cache Redis (key = model+content+op).
  - Qdrant payload thêm `keywords`, `questions`.
  - Worker index gọi enhancement sau embed, trước upsert.
- **Phụ thuộc:** Redis đã có (config). Cần rate limit LLM.

## Phase B — Vận hành & quan sát (P1.9)

### B1. Health check (P1.9)
- Endpoint `GET /health` (không auth) kiểm tra: postgres ping, qdrant collection, seaweedfs bucket, rabbitmq.
- Trả 200 nếu tất cả OK, 503 nếu có thành phần chết, kèm chi tiết.
- Dùng cho Docker/k8s liveness/readiness.

## Phase C — Test & chất lượng (P2.2, P2.3)

### C1. Unit test category (P2.2)
- Test cascade delete, move, cycle detection, cross-user isolation ở tầng service (mock repo).
- Mục tiêu coverage ≥ 80% cho `category_service.go`.

### C2. E2E test category API (P2.3)
- Script Go test dùng httptest + DB test container (hoặc test DB thật).
- Chạy trong CI (`.github/workflows/ci.yml`).

## Phase D — Dài hạn (P2.4)

### D1. Knowledge Compilation
- Nghiên cứu compile doc → Wiki/Graph/Tree/PageIndex; tái sử dụng artifact.
- Chỉ bắt đầu sau khi Phase A ổn định.

## Thứ tự thực hiện đề xuất

1. **B1 (health check)** — nhỏ, giá trị vận hành cao, làm trước để có observability.
2. **A1 (template chunking)** — nền tảng cho A2/A3; làm kỹ vì đụng data.
3. **C1 (unit test category)** — khóa regression trước khi đụng chunker.
4. **A2 (dual embedding)** — sau A1.
5. **A3 (content enhancement)** — sau A2.
6. **C2 (E2E)** — sau khi API ổn định.
7. **D1** — nghiên cứu dài hạn.

## Ghi chú

- Mọi thay đổi chunker đều yêu cầu rebuild vector → dùng `/admin/rebuild-vectors` (đã có, admin-gated).
- Commit theo từng phase, push sau khi mỗi phase xanh (build/vet/test/lint).
- Test MCP với profile `u1`/`u2` (user thường) và admin test user tạm (role=1) như đã làm.