# Proposal: Knowledge Compilation (P2.4)

## 1. Tóm tắt

Knowledge Compilation (RAGFlow v0.27+) biến tài liệu không cấu trúc thành các **knowledge artifact** có cấu trúc, tái sử dụng được: Wiki, Graph, Tree, PageIndex, MindMap, Timeline, và Skills. Thay vì chỉ chunk + embed, hệ thống dùng LLM để trích entity/relationship/concept/timeline/hierarchy, tổ chức thành artifact, rồi dùng artifact làm ngữ cảnh phụ cho retrieval/Q&A/agent.

Proposal này đề xuất tích hợp ở mức **Phase 1 (PageIndex + Tree)** trước, vì repo đã có `SplitBlocks` + `Annotate` + `Section` — gần với PageIndex nhất; các artifact khác (Graph/Wiki/MindMap/Timeline) là Phase 2+.

## 2. Học từ RAGFlow

| Artifact | Mô tả | Phù hợp |
|---|---|---|
| Knowledge graph | Entity + relationship | Quan hệ người/tổ chức/sản phẩm |
| Knowledge tree | Phân cấp chủ đề | Cấu trúc chương, phân loại |
| Page index | Giữ cấu trúc chương gốc + vị trí | Manual, spec, report, pháp luật |
| Mind map | Chủ đề trung tâm + nhánh | Tổng quan nhanh |
| Timeline | Sự kiện theo thời gian | Lịch sử, milestone, policy |
| Wiki | Trang kiến thức liên kết | Enterprise knowledge |
| To Skills | Compile thành Skill cho agent | Tái sử dụng bởi agent |

Cơ chế: template-based (global rules + entity/relation spec), pipeline Parser → Chunker → Compiler → Indexer, artifact ở cấp document và dataset, có thể regenerate.

## 3. So sánh với repo hiện tại

Repo đã có:
- `chunker.SplitBlocks` + `Annotate` + `HeaderFor` — đã trích section (Điều/Khoản) và prepend vào chunk. Đây là tiền đề cho PageIndex.
- `domain.VectorPoint.Section` lưu trong Qdrant payload.
- `list_sections`, `read_article` tools cho agent đọc theo mục.
- Worker 2 phase extract/index với checkpoint.

Thiếu:
- Bảng lưu artifact (PageIndex tree, entity, relation).
- LLM extraction step trong pipeline.
- API/viewer cho artifact.
- Dataset-level aggregation (Wiki).

## 4. Đề xuất phạm vi Phase 1 (PageIndex + Tree)

### 4.1 Data model mới
- `article_sections` (hoặc mở rộng `article_pages`): section_id, article_id, parent_section_id, title, start_chunk, end_chunk, summary.
- `article_entities` (optional, Phase 2): entity, type, source_chunk.
- `article_relations` (optional, Phase 2).

### 4.2 Pipeline
- Thêm phase 2 "compile" sau index: đọc sections từ chunk (đã có Section trong Qdrant payload), gọi LLM sinh summary cho mỗi section, lưu `article_sections`.
- PageIndex = tree section → chunk range (đã có từ SplitBlocks + chunk_index).
- Tree = group sections theo heading hierarchy.

### 4.3 API
- `GET /api/v1/articles/:id/sections` — trả PageIndex (đã có qua Qdrant ListSections, có thể promote thành endpoint).
- `GET /api/v1/articles/:id/tree` — trả cây section kèm summary.
- Admin: `POST /admin/compile` để re-compile.

### 4.4 Agent tools
- Mở rộng `list_sections` (đã có) để trả summary.
- Thêm `read_section_summary`.

## 5. Phase 2+ (sau khi Phase 1 ổn định)

- Graph: entity/relation extraction + storage + query.
- Timeline: time extraction + ordering.
- MindMap: topic + branch extraction.
- Wiki: dataset-level aggregation + Markdown export + versioning.
- To Skills: compile artifact thành Skill file cho agent (đã có skill system).

## 6. Rủi ro & chi phí

- LLM cost: mỗi bài cần N calls (1/section). Cache kết quả (Redis) theo content hash.
- Latency: compile chạy async (worker job), không block index.
- Storage: artifact tables tăng dung lượng nhưng nhỏ hơn vector.
- Migration: thêm bảng mới, không đổi bảng cũ.

## 7. Thứ tự thực hiện đề xuất

1. Phase 1a: PageIndex endpoint promote từ Qdrant ListSections (nhỏ, không LLM).
2. Phase 1b: section summary qua LLM + cache (worker compile job).
3. Phase 1c: Tree view API.
4. Phase 2: Graph (entity/relation) — cần schema + extraction prompt.
5. Phase 3: Timeline, MindMap.
6. Phase 4: Wiki + To Skills (dataset-level).

## 8. Quyết định cần user chốt

- Có muốn bắt đầu Phase 1a (PageIndex endpoint) trong session tiếp không?
- Có ưu tiên Graph hay Timeline trước (tùy use case: pháp luật → PageIndex/Tree; sự kiện → Timeline; tri thức doanh nghiệp → Wiki/Graph)?
- Ngân sách LLM cho compile (mỗi bài ~N calls) — có bật mặc định không?

## 9. Tham khảo

- RAGFlow Knowledge Compilation: https://ragflow.io/docs/knowledge_compilation/overview
- RAGFlow v0.27 blog: https://ragflow.io/blog/ragflow-0.27-knowledge-compilation-and-agentic-retrieval
- Repo: `src/common/chunker/section.go` (SplitBlocks, Annotate, HeaderFor), `src/agent/eino` (list_sections tool).