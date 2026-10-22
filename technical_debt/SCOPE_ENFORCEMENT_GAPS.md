# Lỗ hổng scope category của agent (4 GAP)

Trạng thái: **4/4 đã fix (22/10/2026)** — bộ test regression chạy mặc định,
không còn gate `RUN_SCOPE_GAP_TESTS`; tháo fix là test đỏ.

Bối cảnh: request chat có `category_ids` → `chat_service` đặt
`eino.Scope{UserID, CategoryIDs}` vào ctx (`src/agent/usecase/chat_service.go:93-96`).
Nguyên tắc đã chốt trong `src/agent/eino/context.go`: **DTO thắng LLM**, mọi tool
đọc dữ liệu phải truyền `scope.CategoryIDs` xuống filter Qdrant
(`permissionMusts` luôn có `user_id` + `category_id`).

Hiện trạng: `knowledge_search`, `read_article`, `read_context`, `list_sections`
đã ép đúng scope (test + live test xác nhận). 4 lỗ hổng metadata dưới đây đã
được fix; nội dung chunk vẫn do Qdrant chặn như trước.

| ID | Vị trí (sau fix) | Hành vi sai (trước fix) | Mức | Test | Trạng thái |
|---|---|---|---|---|---|
| GAP-1 | `src/agent/eino/read_context.go` — `NewArticleInfoTool`, nhánh `category_id` | LLM truyền `category_id` ghi đè scope DTO | P1 | `TestScopeGap1ArticleInfoLLMCategoryCannotEscapeScope` | ✅ Đã fix |
| GAP-2 | `src/agent/eino/read_context.go` — `NewArticleInfoTool`, nhánh `article_id` | `article_info` 1 bài chỉ kiểm owner, không kiểm category | P1 | `TestScopeGap2ArticleInfoSingleArticleOutsideScope` | ✅ Đã fix |
| GAP-3 | `src/agent/eino/read_article.go` + `qdrant.CountByFilter` | `CountByArticle` không filter user/category → lộ `total_chunk` | P2 | `TestScopeGap3ReadArticleDoesNotLeakTotalChunk` | ✅ Đã fix |
| GAP-4 | `src/agent/eino/list_categories.go` | `GetAll` trả mọi chủ đề của user, bỏ qua scope | P1 | `TestScopeGap4ListCategoriesRespectsScope` | ✅ Đã fix |

---

## GAP-1 — `article_info` cho LLM ghi đè scope

**Code**

```go
ids := scope.CategoryIDs
if in.CategoryID > 0 {
    ids = []int64{int64(in.CategoryID)}   // LLM thắng DTO — trái quy ước context.go
}
```

**Tái hiện:** scope `[109]`, LLM gọi `article_info{category_id: 108}` → trả
metadata bài của 108 (vẫn là chủ đề của chính user, nhưng nằm ngoài phạm vi
request).

**Ảnh hưởng:** user thu nhỏ phạm vi nhưng agent vẫn đọc được metadata mọi chủ đề
của chính họ; nội dung vẫn bị Qdrant chặn nên không lộ chunk.

**Fix (đã áp dụng)**

```go
ids := scope.CategoryIDs
if in.CategoryID > 0 {
    if !scope.AllowsCategory(int64(in.CategoryID)) {
        return []articleInfoItem{}, nil
    }
    ids = []int64{int64(in.CategoryID)}
}
```

---

## GAP-2 — `article_info` 1 bài không kiểm tra category

**Code**

```go
a, err := repo.GetByID(ctx, int64(in.ArticleID), scope.UserID) // chỉ owner
```

**Tái hiện:** scope `[109]`, `article_info{article_id: <bài thuộc 108>}` → trả
tên file, mô tả, số trang, phiên bản. Qdrant không can thiệp vì đây là query
Postgres.

**Ảnh hưởng:** rò rỉ metadata ngoài phạm vi; agent có thể dùng số trang/mô tả
để trả lời câu hỏi về tài liệu ngoài scope.

**Fix (đã áp dụng)**

```go
if !scope.AllowsCategory(a.CategoryID) {
    return []articleInfoItem{}, nil
}
```

Đã gom logic vào helper `Scope.AllowsCategory` (`src/agent/eino/context.go`)
dùng chung GAP-1/2/4; scope rỗng vẫn cho phép mọi category của user.

---

## GAP-3 — `read_article` lộ `total_chunk` ngoài scope

**Code**

```go
total, err := vectors.CountByArticle(ctx, int64(in.ArticleID)) // chỉ filter article_id
```

`ScrollRange` đã có filter scope nên `items` rỗng với bài ngoài phạm vi, nhưng
`CountByArticle` đếm toàn cục.

**Tái hiện:** scope `[109]`, `read_article{article_id: 47}` (bài thuộc 108) →
`items: []`, `total_chunk: 723` (số chunk thật của bài 47).

**Ảnh hưởng:** lộ quy mô tài liệu ngoài phạm vi; agent có thể nói "file có N
chunk" dù không được phép đọc.

**Fix (đã áp dụng):** thêm method `CountByFilter(ctx, domain.SearchFilter)` vào
`IVectorStore`; Qdrant dùng chung `searchFilter`/`permissionMusts`; `read_article`
gọi `CountByFilter(ctx, filter)` với đúng filter của `ScrollRange`. Giữ
`CountByArticle` cho worker (guard nội bộ, không scope). Đã sửa fake + assert
filter trong `read_article_test.go`; integration test Qdrant kiểm cả user và
category (sai user/category → 0).

Không dùng fix tạm `if len(chunks) == 0 { total = 0 }`: nó che mất trường hợp
bài trong scope đọc quá cuối (trang rỗng hợp lệ vẫn cần tổng).

---

## GAP-4 — `list_categories` trả mọi chủ đề của user

**Code**

```go
cats, err := repo.GetAll(ctx, scope.UserID) // bỏ qua scope.CategoryIDs
```

**Tái hiện (live, đã chạy thật):** scope `[109]`, hỏi "Trong phạm vi chủ đề đang
chọn có những tài liệu nào?" → agent trả "bạn chưa chỉ định rõ đang chọn chủ đề
nào" và liệt kê **cả 106, 107, 108, 109**.

**Ảnh hưởng:** agent mời user chọn chủ đề ngoài phạm vi; là nguồn gốc của hành
vi sai khi user thu nhỏ data.

**Fix (đã áp dụng)**

```go
for _, c := range cats {
    if c == nil || !scope.AllowsCategory(c.ID) {
        continue
    }
    out = append(out, listCategoriesItem{...})
}
```

Lọc ngay trong vòng map sang item (không cấp phát map phụ); mô tả tool cũng đã
thêm câu "Nếu request đã khoanh phạm vi chủ đề, tool chỉ trả các chủ đề trong
phạm vi đó".

Fix này **chỉ lọc ở tầng tool**, không đổi `GetAll` — nhánh API list category
(ngoài agent) vẫn thấy đủ chủ đề của user như cũ.

---

## Việc liên quan (không nằm trong 4 GAP)

- **Prompt chưa biết scope:** `prompts/chat_system.md` không có khái niệm request
  đã khoanh chủ đề; mục "Câu hỏi về chủ đề" còn khuyên gọi `list_categories` và
  "không khớp thì tìm mọi chủ đề". Hiện được bù bằng mô tả tool (mỗi tool nói
  rõ "chỉ trong phạm vi request"); **vẫn nên** chèn block động "Phạm vi tài liệu
  được phép: {tên chủ đề} (id …)" vào instruction khi `len(dto.CategoryIDs) > 0`
  để LLM không phải suy từ mô tả tool. Chưa làm.
- ~~**Test hole:** `read_article_test.go` fake chỉ bắt `from/limit`~~ — đã fix:
  fake giờ có `gotFilter`/`gotCountFilter`/`countByArticleCalls` và assert
  `CountByFilter` được gọi (không dùng lại `CountByArticle`), tháo
  `CategoryIDs: scope.CategoryIDs` là test đỏ.
- ~~**Không giới hạn `len(category_ids)`**~~ — đã fix: `CreateArticleRequest` và
  `ChatRequest` thêm `max=100` (`binding:"omitempty,max=100,dive,gt=0"`).

---

## Chạy test

```powershell
# 4 test regression chạy MẶC ĐỊNH (không còn biến môi trường gate)
go test ./src/agent/eino -run TestScopeGap -v

# Toàn bộ package agent + integration Qdrant (cần Docker/Qdrant sống)
go test ./src/agent/eino ./src/infrastructure/qdrant
```

Kết quả **trước fix** (22/10/2026, khi gate còn bật) — dùng làm mốc lịch sử:

| Test | Kết quả | Thông điệp |
|---|---|---|
| GAP-1 | FAIL | nhận 1 bài của chủ đề 8 dù scope `[9]` |
| GAP-2 | FAIL | metadata bài chủ đề 8 lọt qua scope `[9]` |
| GAP-3 | FAIL | `total_chunk=6` dù bài ngoài scope |
| GAP-4 | FAIL | trả 3 chủ đề `[8,9,10]` dù scope `[9]` |

Sau fix (22/10/2026): cả 4 test PASS mặc định; `go build ./...`, `go vet ./...`
và `golangci-lint run ./...` (0 issues) đều sạch; `go test ./src/agent/...
./src/controller/... ./src/infrastructure/...` xanh, gồm integration Qdrant
kiểm `CountByFilter` theo đúng user/category.

## Giữ fix không bị tháo

1. Muốn thêm tool đọc dữ liệu mới → bắt buộc đi qua `ScopeFrom(ctx)` và dùng
   `scope.AllowsCategory(...)` cho mọi nhánh có `category_id`, không tin tham
   số LLM truyền (quy ước ở `src/agent/eino/context.go`).
2. Đổi filter của `ScrollRange` thì phải đổi cả filter của `CountByFilter`
   (cùng một biến `filter`) — hai chỗ lệch nhau là lộ `total_chunk` lại.
3. Chạy lại live test: scope `[109]` hỏi "trong phạm vi đang chọn có tài liệu
   nào" → chỉ được trả 49/50; hỏi fact của 108 → từ chối kèm lý do "ngoài phạm
   vi chủ đề đang chọn".

---

## Chưa làm — QoS / global concurrency (cần nghiên cứu)

Hệ thống hiện chỉ có **backpressure theo pool** (`EXTRACT_POOL_SIZE`,
`INDEX_POOL_SIZE`, `*_CLAIM_LIMIT`) — không có giới hạn tải toàn cục hay ưu
tiên:

- Không có giới hạn **số request chat đồng thời** trên API; mỗi request đều gọi
  LLM + embedding nên 1000 user hỏi cùng lúc sẽ dồn hết vào Ollama/provider,
  không có queue, không có 429 chủ động.
- Không có **fair-share theo user**: một user upload 100 file có thể chiếm hết
  slot `INDEX_POOL_SIZE`, user khác xếp sau (hiện chấp nhận được vì pool = 2 và
  job timeout 45m, nhưng không scale).
- Không có **rate limit** cho upload/presign (`plans/upload-presign-hardening.md`
  mới dừng ở phần hardening presign, chưa có limiter).
- Không có **metrics** (Prometheus/OTel) để biết đang nghẽn ở đâu; hiện chỉ có
  log + trace_id.

Vì sao **deferred**: chọn mô hình QoS (semaphore trong process vs. queue ngoài
như Redis/RabbitMQ vs. gateway rate limit) ảnh hưởng kiến trúc deploy, cần đo tải
thật trước khi chốt. Khi làm, tối thiểu phải có: semaphore giới hạn request
chat/embed đồng thời, 429 + `Retry-After` khi vượt, và metric queue depth.
