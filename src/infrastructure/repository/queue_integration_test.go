package repository

// Test integration queue chunk/embed trên Postgres thật — chạy cùng DB dev
// local, tự SKIP khi không tới được DB (CI không có Postgres vẫn xanh).
// Dọn sạch row test sau mỗi case bằng storage_key prefix riêng.

import (
	"context"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"
	"knowledge_ingestion/src/infrastructure/postgres"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// queueDBConfig override mỗi database config (host tới được), còn lại ủy
// thác config file — cùng pattern testConfig của postgres.
type queueDBConfig struct {
	inner config.IConfig
	db    config.DBConfig
}

func (c *queueDBConfig) GetApp() config.AppConfig             { return c.inner.GetApp() }
func (c *queueDBConfig) GetDatabase() config.DBConfig         { return c.db }
func (c *queueDBConfig) GetStorage() config.S3Config          { return c.inner.GetStorage() }
func (c *queueDBConfig) GetEmbedding() config.EmbeddingConfig { return c.inner.GetEmbedding() }
func (c *queueDBConfig) GetQdrant() config.QdrantConfig       { return c.inner.GetQdrant() }
func (c *queueDBConfig) GetOCR() config.OCRConfig             { return c.inner.GetOCR() }
func (c *queueDBConfig) GetRedis() config.RedisConfig         { return c.inner.GetRedis() }
func (c *queueDBConfig) GetRabbitMQ() config.RabbitMQConfig   { return c.inner.GetRabbitMQ() }
func (c *queueDBConfig) GetJWT() config.JWTConfig             { return c.inner.GetJWT() }
func (c *queueDBConfig) GetCORS() config.CorsConfig           { return c.inner.GetCORS() }
func (c *queueDBConfig) GetWorker() config.WorkerConfig       { return c.inner.GetWorker() }

// openQueueTestDB mở DB test riêng (manager_test, tự tạo nếu chưa có) qua
// đúng đường migrate production (postgres.NewConnection) — không tới được DB
// hoặc không đủ quyền tạo DB thì SKIP để CI vẫn xanh. DB riêng nên test hoàn
// toàn cô lập, không đụng hàng thật trong DB dev.
func openQueueTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	cfg, err := config.Load(filepath.Join("..", "..", "..", "configs", "config.json"))
	if err != nil {
		t.Skipf("config not available: %v", err)
	}
	db := cfg.GetDatabase()
	host := ""
	for _, h := range []string{db.Host, "127.0.0.1"} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		conn, derr := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(h, strconv.Itoa(db.Port)))
		cancel()
		if derr == nil {
			_ = conn.Close()
			host = h
			break
		}
	}
	if host == "" {
		t.Skipf("postgres not reachable, bỏ qua test queue integration")
	}
	devcfg := db
	devcfg.Host = host
	admin, err := postgres.NewConnection(&queueDBConfig{inner: cfg, db: devcfg})
	if err != nil {
		t.Skipf("postgres dev không vào được: %v", err)
	}
	// CREATE DATABASE không chạy trong transaction — Exec autocommit thường.
	// Lỗi "đã tồn tại" (42P04) hoặc thiếu quyền đều bỏ qua/skip êm.
	if err := admin.GetDB().Exec("CREATE DATABASE manager_test").Error; err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			t.Skipf("không tạo được DB test (thiếu quyền?): %v", err)
		}
	}
	testcfg := devcfg
	testcfg.DBName = "manager_test"
	conn, err := postgres.NewConnection(&queueDBConfig{inner: cfg, db: testcfg})
	if err != nil {
		t.Fatalf("NewConnection test db failed: %v", err)
	}
	return conn.GetDB()
}

func queueTestArticle(key string) *domain.Article {
	return &domain.Article{
		StorageKey:  key,
		Name:        "queue test",
		ContentType: "text/plain",
		UserID:      1,
		CategoryID:  1,
	}
}

func cleanupQueueTest(t *testing.T, db *gorm.DB) {
	t.Helper()
	// ArticleChunk KHÔNG có cột storage_key — xóa theo article_id subquery,
	// rồi mới xóa articles. Thứ tự này quan trọng để không sót orphan.
	db.Unscoped().Where("article_id IN (?)",
		db.Unscoped().Model(&domain.Article{}).Select("id").Where("storage_key LIKE 'test-q-%'"),
	).Delete(&domain.ArticleChunk{})
	db.Unscoped().Where("storage_key LIKE 'test-q-%'").Delete(&domain.Article{})
}

func reloadArticle(t *testing.T, db *gorm.DB, id int64) *domain.Article {
	t.Helper()
	var a domain.Article
	if err := db.Unscoped().First(&a, "id = ?", id).Error; err != nil {
		t.Fatalf("reload article %d: %v", id, err)
	}
	return &a
}

func mustCreateArticle(t *testing.T, ar domain.IArticleRepository, key string) *domain.Article {
	t.Helper()
	a := queueTestArticle(key)
	if err := ar.Create(context.Background(), a); err != nil {
		t.Fatalf("create %s: %v", key, err)
	}
	return a
}

// Claim hốt đúng batch, đánh processing, KHÔNG tăng attempts; claim lần 2
// không trùng; hết lease thì hốt lại được.
func TestQueueClaimLease(t *testing.T) {
	db := openQueueTestDB(t)
	t.Cleanup(func() { cleanupQueueTest(t, db) })
	ctx := context.Background()
	ar := NewArticleRepository(db)

	a1 := mustCreateArticle(t, ar, "test-q-claim-1.txt")
	mustCreateArticle(t, ar, "test-q-claim-2.txt")

	got, err := ar.ClaimChunkPending(ctx, 10, time.Hour)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 claimed, got %d", len(got))
	}
	for _, a := range got {
		r := reloadArticle(t, db, a.ID)
		if r.ChunkStatus != domain.QueueProcessing {
			t.Fatalf("article %d status = %q, want processing", a.ID, r.ChunkStatus)
		}
		if r.ChunkAttempts != 0 {
			t.Fatalf("claim không được tăng attempts, got %d", r.ChunkAttempts)
		}
	}
	// Lease còn hiệu lực → claim lần 2 rỗng (không giẫm).
	again, err := ar.ClaimChunkPending(ctx, 10, time.Hour)
	if err != nil {
		t.Fatalf("claim 2: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("lease còn hạn mà hốt lại %d bài", len(again))
	}
	// Giả lập crash: lùi lease về quá khứ → hốt lại được cả 2.
	db.Model(&domain.Article{}).Where("id IN ?", []int64{a1.ID, got[1].ID}).
		Update("chunk_next_retry_at", time.Now().Add(-time.Minute))
	reclaimed, err := ar.ClaimChunkPending(ctx, 10, time.Hour)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if len(reclaimed) != 2 {
		t.Fatalf("quá lease phải hốt lại 2, got %d", len(reclaimed))
	}
}

// MarkError: lỗi thường → pending + backoff + ghi attempts; quá số lần → failed.
func TestQueueMarkErrorBackoff(t *testing.T) {
	db := openQueueTestDB(t)
	t.Cleanup(func() { cleanupQueueTest(t, db) })
	ctx := context.Background()
	ar := NewArticleRepository(db)

	a := mustCreateArticle(t, ar, "test-q-mark.txt")
	before := time.Now()
	if err := ar.MarkChunkError(ctx, a.ID, 1); err != nil {
		t.Fatalf("mark: %v", err)
	}
	r := reloadArticle(t, db, a.ID)
	if r.ChunkStatus != domain.QueuePending || r.ChunkAttempts != 1 {
		t.Fatalf("want pending/attempts=1, got %+v", r)
	}
	if r.ChunkNextRetryAt.Before(before.Add(50*time.Second)) || r.ChunkNextRetryAt.After(before.Add(70*time.Second)) {
		t.Fatalf("backoff lần 1 phải ~1 phút, got %v", r.ChunkNextRetryAt.Sub(before))
	}
	if err := ar.MarkChunkError(ctx, a.ID, utils.MaxQueueAttempts); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	if r := reloadArticle(t, db, a.ID); r.ChunkStatus != domain.QueueFailed {
		t.Fatalf("quá số lần phải failed, got %q", r.ChunkStatus)
	}
}

// Embed chỉ hốt bài đã chunk done.
func TestQueueEmbedGating(t *testing.T) {
	db := openQueueTestDB(t)
	t.Cleanup(func() { cleanupQueueTest(t, db) })
	ctx := context.Background()
	ar := NewArticleRepository(db)

	a := mustCreateArticle(t, ar, "test-q-gate.txt")
	got, err := ar.ClaimEmbedPending(ctx, 10, time.Hour)
	if err != nil {
		t.Fatalf("claim embed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("chưa chunk done mà hốt embed %d bài", len(got))
	}
	if err := ar.MarkChunkDone(ctx, a.ID); err != nil {
		t.Fatalf("mark chunk done: %v", err)
	}
	got, err = ar.ClaimEmbedPending(ctx, 10, time.Hour)
	if err != nil {
		t.Fatalf("claim embed 2: %v", err)
	}
	if len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("chunk done rồi phải hốt được, got %v", got)
	}
}

// CreateBatch nguyên tử: 1 row lỗi (PointID quá 36 ký tự) thì rollback hết,
// không kẹt nửa vời. Insert trùng (article_id, chunk_index) thì bỏ qua êm.
func TestQueueCreateBatchAtomic(t *testing.T) {
	db := openQueueTestDB(t)
	t.Cleanup(func() { cleanupQueueTest(t, db) })
	ctx := context.Background()
	ar := NewArticleRepository(db)
	cr := NewArticleChunkRepository(db)

	a := mustCreateArticle(t, ar, "test-q-atomic.txt")
	// PointID sinh mới mỗi lần chạy — hardcode UUID thì lần chạy sau trùng
	// row sót (nếu có) và ON CONFLICT DO NOTHING nuốt mất, test sai.
	newPointID := func() string { return uuid.NewString() }
	mk := func(idx int, pointID string) *domain.ArticleChunk {
		return &domain.ArticleChunk{ArticleID: a.ID, ChunkIndex: idx, Content: "x", PointID: pointID}
	}
	bad := []*domain.ArticleChunk{
		mk(0, newPointID()),
		mk(1, strings.Repeat("x", 40)), // varchar(36) → lỗi
		mk(2, newPointID()),
	}
	if err := cr.CreateBatch(ctx, bad); err == nil {
		t.Fatal("batch lỗi phải trả lỗi")
	}
	var n int64
	db.Model(&domain.ArticleChunk{}).Where("article_id = ?", a.ID).Count(&n)
	if n != 0 {
		t.Fatalf("rollback thiếu: còn %d chunk", n)
	}
	good := []*domain.ArticleChunk{
		mk(0, newPointID()),
		mk(1, newPointID()),
	}
	if err := cr.CreateBatch(ctx, good); err != nil {
		t.Fatalf("batch tốt: %v", err)
	}
	// Insert lại y hệt (double-claim) → bỏ qua êm, vẫn 2 row.
	if err := cr.CreateBatch(ctx, good); err != nil {
		t.Fatalf("insert trùng phải bỏ qua êm: %v", err)
	}
	db.Model(&domain.ArticleChunk{}).Where("article_id = ?", a.ID).Count(&n)
	if n != 2 {
		t.Fatalf("want 2 chunk, got %d", n)
	}
}
