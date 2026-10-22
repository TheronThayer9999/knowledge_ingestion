# Cải tiến Knowledge Ingestion

Tổng hợp các điểm cần cải tiến, học hỏi từ RAGFlow và từ thực tế vận hành.
Mỗi mục có mức ưu tiên: **P0** (làm ngay), **P1** (trung hạn), **P2** (dài hạn).

---

## 1. embed_cache phình to (P0)

### Nguyên nhân
- Hash theo *nội dung chunk* (đã kèm header) → hit rate rất thấp, phần lớn entry có `chunk_count = 1`.
- Không eviction/TTL: `PutMany` chỉ `ON CONFLICT` tăng counter, không có janitor.
- Vector 4KB × N row → bảng phình nhanh, đặc biệt khi rebuild/đổi chunker.
- Redundancy: vector đã lưu trong Qdrant; `embed_cache` là bản copy thứ 2.

### Giải pháp đề xuất
- **P0.1** ✅ Janitor eviction (PurgeUnused) đã thêm.
- **P0.2** ✅ Hit rate đo: 99.99% count=1 → cache vô dụng.
- **P0.3** ✅ Admin endpoint `Stats`/`Purge` exposed.
- **P1.1** Template-based chunking theo loại tài liệu → ít chunk hơn → cache nhỏ hơn.
- **P2.1** Cân nhắc bỏ `embed_cache` entirely, dựa vào Qdrant + checkpoint `index_done_chunks` để resume. Ragflow chọn hướng này (không cache vector global).

---

## 2. Chunking chưa tối ưu (P1)

### Hiện trạng
- 1 recursive splitter (Eino) cho mọi file.
- Không có full-text tokenization song song với vector.

### Học từ Ragflow
- **Template-based chunking**: General, Q&A, Table, Paper, Book, Laws, Presentation, Picture, One, Tag, Resume, Manual — mỗi loại tài liệu một chiến lược, giải thích được.
- **Dual embedding**: title vector + content vector kết hợp theo weight (`filename_embd_weight`).
- **Full-text tokenization** (`content_ltks`, `content_sm_ltks`) song song với vector → hybrid search tốt hơn.

### Giải pháp
- **P1.2** ✅ Template-based chunking: DocType field + chunker template (laws qua SplitBlocks). Các loại khác delegate base.
- **P1.3** ✅ Dual embedding: named vectors content+title, RRF fusion trong Qdrant adapter. (Full-text tokenization còn lại là P1.3b.)

---

## 3. Content enhancement (P1)

### Hiện trạng
- Chưa có LLM enhancement cho chunk.

### Học từ Ragflow
- LLM sinh keywords/questions/tags lưu trên chunk → retrieval tốt hơn.
- Cache kết quả LLM trong Redis (`chunkcache`), key = model+content+operation.

### Giải pháp
- **P1.4** ✅ Auto keywords cho chunk (Ollama + Redis cache), Keywords payload. (questions/tags còn lại.)

---

## 4. Authorization chưa đầy đủ (P1)

### Hiện trạng
- Role-based chưa implement: `User.Role` có trong model nhưng không có middleware/check.
- Endpoint admin (`/admin/rebuild-vectors`, `/admin/reset-extract`, `/admin/queue`) chỉ cần đăng nhập.
- Không có rate limit.

### Giải pháp
- **P1.5** ✅ Thêm middleware `RequireRole(role)` cho admin group (role >= 1).
- **P1.6** ✅ `JWT_SECRET` env override (đã có).
- **P1.7** ✅ Rate limit cho login (5 req/phút/IP).

---

## 5. Category management (đã làm)

### Đã hoàn thành
- Cascade delete: xoá folder cha → xoá toàn bộ subtree + bài viết trong cùng transaction.
- Closure table cho hierarchy (multi-root, move, cycle detection).
- `Exists` thay `GetById` cho validate parent (nhẹ hơn).
- Cross-user isolation (owner-scoping) đã test pass.
- Data consistency verified (0 orphan, 0 cycle, 0 missing self-row).

### Còn có thể cải tiến
- **P2.2** ✅ Unit test category (create/move/cycle/delete/cascade/cross-user).
- **P2.3** ✅ E2E test category API (build tag e2e, chạy với `-tags e2e`).

---

## 6. Knowledge Compilation (P2)

### Học từ Ragflow
- Compile doc thành Wiki/Graph/Tree/PageIndex/MindMap/Timeline/Skill — tái sử dụng, giảm xử lý lặp.

### Giải pháp
- **P2.4** Nghiên cứu Knowledge Compilation để tái sử dụng knowledge artifacts.

---

## 7. Data consistency tooling (P1)

### Hiện trạng
- Đã verify bằng SQL thủ công.

### Giải pháp
- **P1.8** ✅ Admin endpoint `/admin/consistency` kiểm tra orphan/cycle/missing self-row.
- **P1.9** ✅ `/health` endpoint kiểm tra postgres/qdrant/seaweedfs/rabbitmq (200/503).

---

## Thứ tự thực hiện đề xuất

1. **P0.1 + P0.2 + P0.3**: Janitor eviction + đo hit rate + admin endpoint (giải quyết phình to ngay).
2. **P1.5 + P1.6 + P1.7**: Role-gating + JWT secret + rate limit (bảo mật).
3. **P1.2 + P1.3**: Template-based chunking + dual embedding (chất lượng retrieval).
4. **P1.4**: Content enhancement.
5. **P1.8 + P1.9**: Data consistency tooling.
6. **P2.***: Knowledge Compilation, test automation.

---

## Tham khảo

- RAGFlow: https://github.com/infiniflow/ragflow
- DeepWiki RAGFlow: https://deepwiki.com/infiniflow/ragflow
- Ragflow chunking templates: docs/guides/dataset/configure_knowledge_base.md
- Ragflow content enhancement: DeepWiki 6.3
