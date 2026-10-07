-- Tạo bảng categories
CREATE TABLE IF NOT EXISTS categories (
                                          id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
                                          user_id     BIGINT NOT NULL, -- owner: mọi query scope theo cột này
                                          name        TEXT NOT NULL,
                                          description TEXT NULL,
                                          parent_id   BIGINT NULL, -- Khớp với *int64 trong Go

                                          created_at  TIMESTAMPTZ DEFAULT NOW(),
    updated_at  TIMESTAMPTZ DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ NULL
    );

-- DB đã tồn tại từ trước thì chạy thêm câu này để bổ sung cột owner:
-- ALTER TABLE categories ADD COLUMN IF NOT EXISTS user_id BIGINT NOT NULL DEFAULT 0;

-- Index lọc theo owner (mọi query danh mục đều có user_id)
CREATE INDEX IF NOT EXISTS idx_categories_user_id ON categories(user_id);

-- 1. Index cho parent_id (Tối ưu query tìm con của một cha)
CREATE INDEX IF NOT EXISTS idx_categories_parent_id ON categories(parent_id);

-- 2. Foreign Key chống "con mồ côi" + RESTRICT xóa cha khi còn con
ALTER TABLE categories
    ADD CONSTRAINT fk_categories_parent
        FOREIGN KEY (parent_id) REFERENCES categories(id)
            ON DELETE RESTRICT;

-- 3. Unique Partial Index: mỗi user DUY NHẤT 1 Root (parent_id IS NULL)
DROP INDEX IF EXISTS idx_categories_single_root;
CREATE UNIQUE INDEX IF NOT EXISTS idx_categories_single_root_per_user
    ON categories (user_id) WHERE parent_id IS NULL;


ALTER TABLE articles
    ADD CONSTRAINT fk_articles_category
        FOREIGN KEY (category_id) REFERENCES categories(id)
            ON DELETE RESTRICT;