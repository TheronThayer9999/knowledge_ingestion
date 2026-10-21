# Kế hoạch: thay `parent_id` bằng closure table cho categories

Trạng thái: **chưa bắt đầu**.
Ngày lập: 2026-10-21.

## 1. Quyết định đã chốt

| Vấn đề | Chốt |
|---|---|
| Mô hình | **Pure closure** — bỏ hẳn cột `categories.parent_id`, `category_closure` là nguồn sự thật duy nhất |
| Root | **Forest** — mỗi user có nhiều root, bỏ unique index `idx_categories_single_root_per_user` |
| API | `parent_id` vẫn là tham số **đầu vào** của request (chỉ dùng để ghi closure), không còn là cột của `categories` |
| Response | Vẫn trả `parent_id` (suy ra từ closure `depth = 1`) + thêm `depth` để FE dựng cây |

## 2. Bug hiện có phải sửa trước khi thêm tính năng

1. **Repo không tham gia transaction.** `InTx` nhét `*gorm.DB` vào ctx qua `txKey`, helper `dbConn(ctx, db)` đã có ở `src/infrastructure/repository/unit_of_work.go`, nhưng `category_repository.go` và `category_closure_repository.go` gọi thẳng `db.WithContext(ctx)`. Hệ quả: mọi lệnh ghi của category nằm **ngoài** transaction, rollback vô hiệu. Mọi method phải đổi sang `dbConn(ctx, t.db)` (giống `articles_repository.go`).
2. `Create` ghi closure **trước khi** category có ID và hardcode `AncestorID/DescendantID = 1` → trỏ sai node.
3. `Update` đổi `ParentID` nhưng không đụng closure.
4. `Delete` không xoá closure row.
5. `GetDescendants`, `GetAncestors`, `CheckExist`, `CheckOrphaned` đang `panic("implement me")`.
6. `CategoryRepository.Update` dùng `Model(topic).Updates(topic)` với struct → gorm bỏ qua zero value, không thể set `description = ""`.

## 3. Lược đồ dữ liệu

### 3.1 `category_closure`

| Cột | Kiểu | Ghi chú |
|---|---|---|
| `ancestor_id` | BIGINT | FK → `categories(id)`, phần 1 của PK composite |
| `descendant_id` | BIGINT | FK → `categories(id)`, phần 2 của PK composite |
| `depth` | INT NOT NULL | 0 = self, 1 = con, 2 = cháu… |

- PK composite `(ancestor_id, descendant_id)` — chặn trùng dòng.
- Index riêng trên `descendant_id` (PK chỉ tối ưu theo `ancestor_id`).
- **Không** thêm `user_id` vào bảng này: quyền được kiểm ở `categories` trước khi thao tác; thêm cột owner vào đây là dữ liệu lặp và dễ lệch.
- FK `ON DELETE CASCADE` chỉ là lưới an toàn (categories xoá mềm, service phải tự xoá closure).

Thay đổi ở `src/domain/category_closure.go`:

```go
type CategoryClosure struct {
    AncestorID   int64 `json:"ancestor_id" gorm:"primaryKey"`
    DescendantID int64 `json:"descendant_id" gorm:"primaryKey;index"`
    Depth        int   `json:"depth"`
}

type CategoryWithDepth struct {
    Category
    Depth    int    `json:"depth"`
    ParentID *int64 `json:"parent_id,omitempty"`
}
```

### 3.2 `categories`

- Xoá field `ParentID *int64` khỏi `domain.Category`.
- Xoá các cột/index liên quan ở DB: `parent_id`, `fk_categories_parent`, `idx_categories_parent_id`, `idx_categories_single_root_per_user`.
- Giữ nguyên `user_id`, `type`, `articles.category_id` FK → `categories(id)`.

## 4. Hợp đồng repository

### 4.1 `ICategoryClosureRepositoryImpl` (mới)

```go
CreateNode(ctx context.Context, nodeID int64, parentID *int64) error
MoveSubtree(ctx context.Context, nodeID, newParentID int64) error
DeleteByNode(ctx context.Context, nodeID int64) error
CountDirectChildren(ctx context.Context, nodeID int64) (int64, error)
GetByOwner(ctx context.Context, userID int64) ([]*CategoryClosure, error)
GetDescendants(ctx context.Context, ancestorID, userID int64) ([]*CategoryWithDepth, error)
GetAncestors(ctx context.Context, descendantID, userID int64) ([]*CategoryWithDepth, error)
CheckExist(ctx context.Context, ancestorID, descendantID int64) (bool, error)
```

- Bỏ `Create(ctx, *CategoryClosure)`: caller không bao giờ được tự ghép cặp, dễ tái diễn bug hardcode.
- Bỏ `CheckOrphaned`: không nơi nào gọi, ngữ nghĩa chưa định nghĩa. Cần thì mở lại sau.
- `GetDescendants`/`GetAncestors`/`GetByOwner` nhận `userID` để join `categories` lọc owner + `deleted_at IS NULL`.

### 4.2 `ICategoryRepositoryImpl`

```go
Create(ctx, *Category) error
Update(ctx, *Category) error          // sửa để ghi được zero value
Delete(ctx, *Category) error
GetById(ctx, id, userID int64) (*Category, error)
GetByName(ctx, name string, userID int64) (*Category, error)
GetAllWithMeta(ctx, userID int64) ([]*CategoryWithDepth, error)   // thay GetAll
GetDirectParent(ctx, nodeID int64) (*int64, error)
```

`GetAll` cũ bị thay bằng `GetAllWithMeta` để response có `depth` + `parent_id` trong **một** query, không N+1.

## 5. SQL chuẩn cho từng thao tác

### 5.1 Create node (`CreateNode`)

```sql
INSERT INTO category_closure (ancestor_id, descendant_id, depth)
VALUES (@id, @id, 0);

INSERT INTO category_closure (ancestor_id, descendant_id, depth)
SELECT ancestor_id, @id, depth + 1
FROM category_closure
WHERE descendant_id = @parentID
ON CONFLICT DO NOTHING;
```

`@id` phải lấy **sau** khi `categoryRepo.Create` chạy (gorm nhận ID qua `RETURNING`). Toàn bộ trong 1 transaction.

### 5.2 Move subtree (`MoveSubtree`)

```sql
-- cắt subtree khỏi tổ tiên cũ
DELETE FROM category_closure
WHERE descendant_id IN (SELECT descendant_id FROM category_closure WHERE ancestor_id = @node)
  AND ancestor_id NOT IN (SELECT descendant_id FROM category_closure WHERE ancestor_id = @node);

-- nối subtree vào tổ tiên mới (bao gồm ancestor của @newParent và chính @newParent)
INSERT INTO category_closure (ancestor_id, descendant_id, depth)
SELECT sup.ancestor_id, sub.descendant_id, sup.depth + sub.depth + 1
FROM category_closure sup
JOIN category_closure sub ON sub.ancestor_id = @node
WHERE sup.descendant_id = @newParent
ON CONFLICT DO NOTHING;
```

Chặn cycle trước khi gọi: `CheckExist(node, newParent) == true` nghĩa là `newParent` nằm trong subtree của `node` → trả 400.

### 5.3 Delete node (`DeleteByNode`)

```sql
DELETE FROM category_closure
WHERE descendant_id = @id OR ancestor_id = @id;
```

Luật "còn con thì không xoá" kiểm bằng:

```sql
SELECT count(*) FROM category_closure WHERE ancestor_id = @id AND depth = 1;
```

### 5.4 List kèm meta (`GetAllWithMeta`)

```sql
SELECT c.*, cc.depth, p.id AS parent_id
FROM categories c
LEFT JOIN category_closure cc ON cc.descendant_id = c.id AND cc.depth = 1
LEFT JOIN categories p ON p.id = cc.ancestor_id AND p.deleted_at IS NULL
WHERE c.user_id = ? AND c.deleted_at IS NULL
ORDER BY cc.depth NULLS FIRST, c.created_at ASC, c.id ASC;
```

## 6. Service (`category_service.go`)

### Create
1. `ParentID != nil` → `categoryRepo.GetById(ctx, *ParentID, userID)`; `domain.ErrNotFound` → 400 "danh mục cha không tồn tại".
2. `unitOfWork.InTx`:
   - `categoryRepo.Create(txCtx, category)` (gorm gán `category.ID`).
   - `categoryClosureRepo.CreateNode(txCtx, category.ID, dto.ParentID)`.
3. Trả `ToCategoryResponse` với `Depth`/`ParentID` (root → `ParentID = nil`, `Depth = 0`).

### Update
1. Load node owner-scoped → 404.
2. Nếu request có `ParentID`:
   - `*ParentID == node.ID` → 400.
   - `GetById(*ParentID, userID)` → 404/400 nếu không tồn tại hoặc khác owner.
   - `CheckExist(node.ID, *ParentID)` → 400 "không thể chuyển danh mục vào chính nhánh con của nó".
   - `MoveSubtree` trong transaction.
3. Cập nhật `Name`/`Description` (kể cả chuỗi rỗng) bằng `Select` tường minh, không `Updates(struct)`.

**Vấn đề tri-state của `parent_id`:** `UpdateCategoryRequest.ParentID *int64` không phân biệt được "không gửi" với "gửi null" (move về root). Cần một trong hai:
- (khuyến nghị) thêm `dtos.OptionalInt64{ Set bool; Value *int64 }` với `UnmarshalJSON` — `Set` chỉ true khi key xuất hiện trong body, kể cả `null`.
- hoặc thêm field `MoveToRoot bool` bên cạnh `ParentID *int64`.

### Delete
1. Load node owner-scoped → 404.
2. `CountDirectChildren > 0` → 400 "danh mục còn danh mục con".
3. Transaction: `DeleteByNode` → `categoryRepo.Delete`.
4. Còn `articles.category_id` tham chiếu → FK RESTRICT trả lỗi; map 400 với message riêng ("còn tài liệu trong danh mục"), đừng dùng chung message với "còn danh mục con".

### GetAll
- Dùng `GetAllWithMeta`; response trả list phẳng kèm `depth` + `parent_id` (FE tự dựng cây). Nested tree là follow-up, không làm ở đợt này.

### GetById / GetByName
- Sau khi load category, lấy `GetDirectParent` để điền `parent_id` cho response.

## 7. Migration (`src/infrastructure/postgres/migrate.go`)

Thêm `migrateCategoryClosure(db)` chạy sau `AutoMigrate`, idempotent theo từng bước:

1. **Backfill** — chỉ chạy khi `hasColumn(db, "categories", "parent_id")`:

```sql
TRUNCATE category_closure;

WITH RECURSIVE tree AS (
    SELECT id, id AS ancestor_id, 0 AS depth
    FROM categories
    WHERE parent_id IS NULL AND deleted_at IS NULL
  UNION ALL
    SELECT c.id, t.ancestor_id, t.depth + 1
    FROM categories c
    JOIN tree t ON c.parent_id = t.id
    WHERE c.deleted_at IS NULL
)
INSERT INTO category_closure (ancestor_id, descendant_id, depth)
SELECT ancestor_id, id, depth FROM tree
ON CONFLICT DO NOTHING;
```

`TRUNCATE` là bắt buộc đợt đầu: dữ liệu closure hiện tại do bug hardcode `1` nên không dùng được. Chỉ truncate khi `parent_id` còn — tức đúng một lần.

2. **PK composite** — thêm helper `hasPrimaryKey(db, table)` (query `information_schema.table_constraints`), rồi `ALTER TABLE category_closure ADD PRIMARY KEY (ancestor_id, descendant_id)`.
3. **Index**: `CREATE INDEX IF NOT EXISTS idx_category_closure_descendant ON category_closure (descendant_id)`.
4. **FK** (kiểm tra tồn tại constraint trước khi `ALTER TABLE ... ADD CONSTRAINT`): `ancestor_id` và `descendant_id` → `categories(id) ON DELETE CASCADE`.
5. **Drop** `categories.parent_id` sau khi mọi bước trên xong. Postgres tự bỏ kèm `fk_categories_parent`, `idx_categories_parent_id`, `idx_categories_single_root_per_user`; vẫn nên `DROP INDEX IF EXISTS` các index này cho log rõ ràng.

Thứ tự bắt buộc: backfill **trước** drop. Nếu backfill lỗi, dừng boot (fail-fast) thay vì drop cột.

## 8. DTO / API

- `CreateCategoryRequest.ParentID *int64` — giữ nguyên (input).
- `UpdateCategoryRequest.ParentID` — đổi sang `OptionalInt64` (hoặc thêm `MoveToRoot`).
- `CategoryResponse` — thêm `Depth int`; `ParentID` vẫn có nhưng là giá trị suy ra.
- Mapper: `ToCategoryResponse(*domain.CategoryWithDepth)`; `ToCategoryListResponse([]*domain.CategoryWithDepth)`.
- Swagger: regenerate `swag init -g cmd/apis/main.go` sau khi đổi DTO/annotation (không sửa tay `docs/`).
- `schema/category.sql` cập nhật theo lược đồ mới (bỏ `parent_id`, thêm `category_closure`).

## 9. Danh sách file thay đổi

| File | Việc |
|---|---|
| `src/domain/category.go` | Bỏ `ParentID`, đổi interface `GetAll` → `GetAllWithMeta`, thêm `GetDirectParent` |
| `src/domain/category_closure.go` | Gorm tag PK/index, `CategoryWithDepth.ParentID`, interface mới |
| `src/infrastructure/repository/category_repository.go` | `dbConn`, sửa `Update`, `GetAllWithMeta`, `GetDirectParent` |
| `src/infrastructure/repository/category_closure_repository.go` | Implement toàn bộ method, bỏ panic |
| `src/controller/services/category_service.go` | Create/Update/Delete/GetAll theo closure |
| `src/controller/dtos/category_dto.go` | `OptionalInt64`, `Depth`, mapper |
| `src/infrastructure/postgres/migrate.go` | `migrateCategoryClosure` + helper `hasPrimaryKey`/`hasConstraint` |
| `schema/category.sql` | Cập nhật lược đồ tham chiếu |
| `docs/` | Regenerate bằng swag |

## 10. Thứ tự thực hiện (rollback được)

1. Sửa `dbConn` ở 2 repo + fix `Update` zero value (không đổi hành vi API).
2. Implement closure repo + đổi service (vẫn còn `parent_id` trong DB, chưa ai drop gì).
3. Thêm backfill vào `migrate.go`, chạy boot một lần, kiểm tra bảng closure bằng SQL.
4. Test API 3 cấp: create root → child → grandchild, move, delete.
5. Chỉ khi (3) và (4) đúng: bật bước drop `parent_id` + cập nhật `schema/category.sql` + swag.

## 11. Test

- **Repository** (`category_closure_repository_test.go`, skip nếu không kết nối được Postgres như `connection_test.go`):
  - create root/child/grandchild → số dòng closure = 1, 3, 6.
  - move subtree sang nhánh khác → tổ tiên cũ mất, tổ tiên mới đúng.
  - `CheckExist(node, con của node)` = true; `CheckExist(node, chính nó)` = true.
  - `DeleteByNode` xoá mọi dòng chạm node, không để mồ côi.
- **Service** (fake repo): cha khác owner → 400; move vào chính con → 400; delete còn con → 400; `parent_id: null` trong update → node thành root.
- **Migration**: dựng vài dòng `categories` có `parent_id`, chạy `migrateCategoryClosure`, so khớp closure.
- Thủ công: `GET /api/v1/categories` trả `depth` + `parent_id` đúng sau mỗi thao tác.

## 12. Tiêu chí hoàn thành

- Không còn field `ParentID` trong `domain.Category` và cột `categories.parent_id` trong DB.
- `category_closure` có PK composite, không có dòng trùng/mồ côi, không còn dòng hardcode `1`.
- Create/move/delete qua API cho cây đúng ở 3 cấp; cycle bị chặn 400.
- `go build ./...`, `go vet ./...`, `golangci-lint run ./...`, `go test ./...` xanh.

## 13. Rủi ro

- **Mất hierarchy nếu drop `parent_id` khi backfill sai.** Backup `categories` + `category_closure` (`pg_dump -t`) trước bước 5. Bước 1–4 chưa mất gì.
- **Dữ liệu cũ có cycle** (nếu từng update tạo vòng) làm recursive CTE chạy vô hạn. Trước khi backfill nên kiểm tra, hoặc dùng `CYCLE id SET is_cycle USING path` (Postgres 14+) và bỏ qua nhánh cycle.
- **Closure không có `user_id`**: mọi node id nhận từ client phải đi qua `categoryRepo.GetById(id, userID)` trước khi ghi closure; không được gọi thẳng `MoveSubtree`/`DeleteByNode` bằng id thô.
- **`GetAll` giờ join 2 bảng**: với vài nghìn category/user vẫn ổn; nếu lớn hơn thì cache hoặc trả closure list riêng cho FE.
