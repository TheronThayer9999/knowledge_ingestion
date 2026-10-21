-- Tạo bảng categories (folder). Quan hệ cha–con KHÔNG nằm ở bảng này mà ở
-- category_closure — nên không còn cột parent_id.
CREATE TABLE IF NOT EXISTS categories (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     BIGINT NOT NULL, -- owner: mọi query scope theo cột này
    name        TEXT NOT NULL,
    description TEXT NULL,

    created_at  TIMESTAMPTZ DEFAULT NOW(),
    updated_at  TIMESTAMPTZ DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ NULL
);

-- DB đã tồn tại từ trước thì chạy thêm câu này để bổ sung cột owner:
-- ALTER TABLE categories ADD COLUMN IF NOT EXISTS user_id BIGINT NOT NULL DEFAULT 0;

-- Index lọc theo owner (mọi query danh mục đều có user_id)
CREATE INDEX IF NOT EXISTS idx_categories_user_id ON categories(user_id);

-- Bảng trung gian closure: mỗi dòng là "ancestor_id là tổ tiên của
-- descendant_id, cách nhau depth cạnh". PK là CẶP id; depth chỉ là thuộc
-- tính của cặp nên KHÔNG nằm trong khoá (nếu nằm trong khoá sẽ cho phép ghi
-- trùng cặp và làm ON CONFLICT mất tác dụng).
CREATE TABLE IF NOT EXISTS category_closure (
    ancestor_id   BIGINT NOT NULL,
    descendant_id BIGINT NOT NULL,
    depth         INTEGER NOT NULL, -- 0 = chính nó, 1 = con, 2 = cháu…
    CONSTRAINT category_closure_pkey PRIMARY KEY (ancestor_id, descendant_id)
);

-- Index cho chiều tra ngược: "cha trực tiếp của X" (descendant_id = X).
CREATE INDEX IF NOT EXISTS idx_category_closure_descendant_id ON category_closure(descendant_id);

-- FK về categories: purge cứng một category thì closure tự sạch, không để
-- lại dòng mồ côi.
ALTER TABLE category_closure
    ADD CONSTRAINT fk_closure_ancestor   FOREIGN KEY (ancestor_id)   REFERENCES categories(id) ON DELETE CASCADE,
    ADD CONSTRAINT fk_closure_descendant FOREIGN KEY (descendant_id) REFERENCES categories(id) ON DELETE CASCADE;

ALTER TABLE articles
    ADD CONSTRAINT fk_articles_category
        FOREIGN KEY (category_id) REFERENCES categories(id)
            ON DELETE RESTRICT;
