package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"knowledge_ingestion/src/common/chunker"
	"knowledge_ingestion/src/domain"
)

// cacheCountingEmbedder đếm số text thật sự đi vào model — đó là thứ cache
// phải làm giảm, nên đây là đại lượng duy nhất test cần đo.
type cacheCountingEmbedder struct {
	indexStubEmbedder
	embedded int
}

func (e *cacheCountingEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	e.embedded += len(texts)
	return e.indexStubEmbedder.EmbedBatch(ctx, texts)
}

// memEmbedCache là EmbedCache trong bộ nhớ. getErr/putErr mô phỏng DB hỏng.
type memEmbedCache struct {
	data   map[string][]byte
	getErr error
	putErr error
}

func newMemEmbedCache() *memEmbedCache {
	return &memEmbedCache{data: map[string][]byte{}}
}

func (m *memEmbedCache) Get(_ context.Context, hashes []string, model string, dim int) (map[string][]float32, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	out := make(map[string][]float32)
	for _, h := range hashes {
		b, ok := m.data[h+"|"+model]
		if !ok {
			continue
		}
		v, err := domain.DecodeVector(b)
		if err != nil {
			continue
		}
		out[h] = v
	}
	return out, nil
}

func (m *memEmbedCache) PutMany(_ context.Context, entries []*domain.EmbedCacheEntry) error {
	if m.putErr != nil {
		return m.putErr
	}
	for _, e := range entries {
		m.data[e.TextHash+"|"+e.Model] = e.Vector
	}
	return nil
}

func (m *memEmbedCache) Stats(context.Context) (int64, int64, error) {
	return int64(len(m.data)), 0, nil
}

func (m *memEmbedCache) PurgeUnused(context.Context, time.Duration) (int64, error) {
	return 0, nil
}

func cachePoints(texts ...string) []*domain.VectorPoint {
	out := make([]*domain.VectorPoint, 0, len(texts))
	for i, t := range texts {
		out = append(out, &domain.VectorPoint{ID: "p" + string(rune('a'+i)), Text: t})
	}
	return out
}

func newCacheSvc(emb *cacheCountingEmbedder, cache domain.EmbedCache) *articleIndexService {
	vec := &indexStubVectors{}
	return NewArticleIndexService(&indexStubArticles{alive: true}, &indexStubPages{}, emb, vec, cache, chunker.Manual{}).(*articleIndexService)
}

// Lần 2 cùng text phải KHÔNG gọi model lần nào — đây là lý do có cache.
func TestUpsertBatchCacheHitSkipsModel(t *testing.T) {
	emb := &cacheCountingEmbedder{}
	cache := newMemEmbedCache()
	svc := newCacheSvc(emb, cache)
	article := &domain.Article{BaseModel: domain.BaseModel{ID: 1}}

	if err := svc.upsertBatch(context.Background(), "t1", article, cachePoints("alpha", "beta", "gamma")); err != nil {
		t.Fatalf("lần 1 lỗi: %v", err)
	}
	if emb.embedded != 3 {
		t.Fatalf("lần 1 embed %d text, want 3", emb.embedded)
	}

	batch := cachePoints("alpha", "beta", "gamma")
	if err := svc.upsertBatch(context.Background(), "t2", article, batch); err != nil {
		t.Fatalf("lần 2 lỗi: %v", err)
	}
	if emb.embedded != 3 {
		t.Errorf("lần 2 gọi model thêm, tổng = %d, want 3 (cache phải hit hết)", emb.embedded)
	}
	for i, p := range batch {
		if len(p.Vector) != 2 {
			t.Errorf("point[%d] vector len = %d, want 2", i, len(p.Vector))
		}
	}
}

// Chỉ phần THIẾU mới được embed.
func TestUpsertBatchEmbedsOnlyMisses(t *testing.T) {
	emb := &cacheCountingEmbedder{}
	cache := newMemEmbedCache()
	svc := newCacheSvc(emb, cache)
	article := &domain.Article{BaseModel: domain.BaseModel{ID: 1}}

	if err := svc.upsertBatch(context.Background(), "t1", article, cachePoints("alpha")); err != nil {
		t.Fatalf("lỗi: %v", err)
	}
	if err := svc.upsertBatch(context.Background(), "t2", article, cachePoints("alpha", "delta")); err != nil {
		t.Fatalf("lỗi: %v", err)
	}
	if emb.embedded != 2 {
		t.Errorf("tổng embed = %d, want 2 (alpha từ cache, chỉ delta là mới)", emb.embedded)
	}
}

// Cache khác model thì không được dùng lại vector.
func TestUpsertBatchCacheKeyedByModel(t *testing.T) {
	cache := newMemEmbedCache()
	emb := &cacheCountingEmbedder{}
	svc := newCacheSvc(emb, cache)
	article := &domain.Article{BaseModel: domain.BaseModel{ID: 1}}
	if err := svc.upsertBatch(context.Background(), "t1", article, cachePoints("alpha")); err != nil {
		t.Fatalf("lỗi: %v", err)
	}

	other := &otherModelEmbedder{cacheCountingEmbedder: &cacheCountingEmbedder{}}
	svc2 := NewArticleIndexService(&indexStubArticles{alive: true}, &indexStubPages{}, other, &indexStubVectors{}, cache, chunker.Manual{}).(*articleIndexService)
	if err := svc2.upsertBatch(context.Background(), "t2", article, cachePoints("alpha")); err != nil {
		t.Fatalf("lỗi: %v", err)
	}
	if other.embedded != 1 {
		t.Errorf("model mới embed %d, want 1 (không được lấy vector của model cũ)", other.embedded)
	}
}

type otherModelEmbedder struct {
	*cacheCountingEmbedder
}

func (otherModelEmbedder) ModelName() string { return "other-model" }

// Cache nil (không cấu hình) vẫn embed đầy đủ, không panic.
func TestUpsertBatchNilCache(t *testing.T) {
	emb := &cacheCountingEmbedder{}
	svc := newCacheSvc(emb, nil)
	article := &domain.Article{BaseModel: domain.BaseModel{ID: 1}}
	if err := svc.upsertBatch(context.Background(), "t1", article, cachePoints("a", "b")); err != nil {
		t.Fatalf("lỗi: %v", err)
	}
	if emb.embedded != 2 {
		t.Errorf("embed %d, want 2", emb.embedded)
	}
}

// Cache đọc lỗi thì coi như trống, job vẫn chạy và vẫn đủ vector.
func TestUpsertBatchCacheGetErrorTolerated(t *testing.T) {
	emb := &cacheCountingEmbedder{}
	cache := newMemEmbedCache()
	cache.getErr = errors.New("db down")
	svc := newCacheSvc(emb, cache)
	article := &domain.Article{BaseModel: domain.BaseModel{ID: 1}}
	batch := cachePoints("a", "b")
	if err := svc.upsertBatch(context.Background(), "t1", article, batch); err != nil {
		t.Fatalf("cache Get lỗi không được fail job: %v", err)
	}
	if emb.embedded != 2 {
		t.Errorf("embed %d, want 2", emb.embedded)
	}
	for i, p := range batch {
		if len(p.Vector) != 2 {
			t.Errorf("point[%d] thiếu vector", i)
		}
	}
}

// Cache ghi lỗi chỉ log, không làm fail job.
func TestUpsertBatchCachePutErrorTolerated(t *testing.T) {
	emb := &cacheCountingEmbedder{}
	cache := newMemEmbedCache()
	cache.putErr = errors.New("db down")
	svc := newCacheSvc(emb, cache)
	article := &domain.Article{BaseModel: domain.BaseModel{ID: 1}}
	if err := svc.upsertBatch(context.Background(), "t1", article, cachePoints("a")); err != nil {
		t.Fatalf("cache PutMany lỗi không được fail job: %v", err)
	}
}

// Vector hỏng trong cache phải coi như miss — embed lại, không trả mảng sai.
func TestUpsertBatchCorruptCacheEntryIsMiss(t *testing.T) {
	emb := &cacheCountingEmbedder{}
	cache := newMemEmbedCache()
	cache.data[chunkHash("a")+"|stub"] = []byte{1, 2, 3}
	svc := newCacheSvc(emb, cache)
	article := &domain.Article{BaseModel: domain.BaseModel{ID: 1}}
	batch := cachePoints("a")
	if err := svc.upsertBatch(context.Background(), "t1", article, batch); err != nil {
		t.Fatalf("lỗi: %v", err)
	}
	if emb.embedded != 1 {
		t.Errorf("embed %d, want 1 (vector hỏng phải embed lại)", emb.embedded)
	}
}

func TestEncodeDecodeVectorRoundTrip(t *testing.T) {
	in := []float32{0.5, -1.25, 3e-7}
	out, err := domain.DecodeVector(domain.EncodeVector(in))
	if err != nil {
		t.Fatalf("decode lỗi: %v", err)
	}
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("[%d] = %v, want %v", i, out[i], in[i])
		}
	}
	if _, err := domain.DecodeVector([]byte{1, 2, 3}); !errors.Is(err, domain.ErrBadVector) {
		t.Errorf("độ dài lẻ phải trả ErrBadVector, got %v", err)
	}
}
