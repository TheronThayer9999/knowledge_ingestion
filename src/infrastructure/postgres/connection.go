package postgres

import (
	"context"
	"fmt"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type IDB interface {
	GetDB() *gorm.DB
}

type dbWrapper struct {
	db *gorm.DB
}

func (w *dbWrapper) GetDB() *gorm.DB {
	return w.db
}

func NewConnection(cfg config.IConfig) (IDB, error) {
	dbCfg := cfg.GetDatabase()

	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		dbCfg.Host, dbCfg.Port, dbCfg.User, dbCfg.Password, dbCfg.DBName, dbCfg.SSLMode,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		// traceLogger giữ ngưỡng SLOW 200ms như gorm default, thêm trace_id
		// vào mỗi dòng để truy từ SQL chậm ngược về request/job gây ra nó.
		Logger: traceLogger{},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres connection: %w", err)
	}
	//add struct migrate
	err = db.AutoMigrate(domain.User{}, domain.Category{}, domain.Article{}, domain.ArticleChunk{}, domain.OutboxEvent{})
	if err != nil {
		return nil, err
	}
	// Index composite partial cho luồng list article (user_id + category_id
	// + order created_at): WHERE deleted_at IS NULL vì mọi query gorm đều
	// kèm điều kiện đó nên index gọn và Sort biến mất khi bảng lớn.
	// AutoMigrate không dựng được partial index nên exec tay, IF NOT EXISTS
	// để boot lại idempotent.
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_articles_owner_list ON articles (user_id, category_id, created_at DESC) WHERE deleted_at IS NULL`).Error; err != nil {
		return nil, fmt.Errorf("failed to create articles owner index: %w", err)
	}
	// Index cho luồng chunk worker (article_id + chunk_index + order đọc
	// lại) và tra cứu "bài đã chunk chưa": WHERE deleted_at IS NULL vì mọi
	// query gorm đều kèm điều kiện đó. Tag index trong model đã dựng 1
	// index, đây là composite cover thêm thứ tự đọc — IF NOT EXISTS để boot
	// lại idempotent.
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_chunks_article_read ON article_chunks (article_id, chunk_index) WHERE deleted_at IS NULL`).Error; err != nil {
		return nil, fmt.Errorf("failed to create article chunks index: %w", err)
	}
	// Unique (article_id, chunk_index) để 2 worker claim trùng không insert
	// trùng chunk — CreateBatch ON CONFLICT DO NOTHING dựa vào index này.
	// Partial (bỏ row đã xóa mềm) để re-chunk bài từng bị xóa không vướng.
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_chunks_article_unique ON article_chunks (article_id, chunk_index) WHERE deleted_at IS NULL`).Error; err != nil {
		return nil, fmt.Errorf("failed to create article chunks unique index: %w", err)
	}
	// Index cho worker poll queue chunk/embed — WHERE deleted_at IS NULL vì
	// mọi query gorm đều kèm điều kiện đó. Embed poll thêm chunk_status done
	// để phase 2 chỉ hốt bài đã chunk xong.
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_articles_chunk_poll ON articles (chunk_status, chunk_next_retry_at, id) WHERE deleted_at IS NULL`).Error; err != nil {
		return nil, fmt.Errorf("failed to create articles chunk poll index: %w", err)
	}
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_articles_embed_poll ON articles (embed_status, embed_next_retry_at, id) WHERE deleted_at IS NULL AND chunk_status = 'done'`).Error; err != nil {
		return nil, fmt.Errorf("failed to create articles embed poll index: %w", err)
	}
	// Backfill queue: bài đã có chunk từ trước khi có cột status (cột mới
	// default pending) thì đánh chunk done luôn để worker không chunk lại gây
	// trùng. Embed để pending cho 1 lượt catch-up idempotent (upsert đúng
	// PointID đã lưu nên không trùng point).
	if err := db.Exec(`UPDATE articles SET chunk_status = 'done' WHERE chunk_status = 'pending' AND id IN (SELECT DISTINCT article_id FROM article_chunks WHERE deleted_at IS NULL)`).Error; err != nil {
		return nil, fmt.Errorf("failed to backfill chunk status: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql pool: %w", err)
	}

	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(ctx); err != nil {
		err := sqlDB.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	logs.Infow("postgresql connected", "host", dbCfg.Host, "port", dbCfg.Port, "db", dbCfg.DBName)

	return &dbWrapper{db: db}, nil
}
