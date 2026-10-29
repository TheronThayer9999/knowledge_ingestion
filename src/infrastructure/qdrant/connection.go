package qdrant

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"

	qdrantapi "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// compile-time check: connection phải implement domain.IVectorStore
var _ domain.IVectorStore = (*connection)(nil)

// quantizationMode là cách nén vector — đọc từ config qdrant.quantization.
// Đặt ở tầng infra (không phải config) để tên hằng dùng chung với builder
// request, tránh chuỗi "scalar"/"binary" rải khắp nơi.
type quantizationMode int

const (
	quantNone quantizationMode = iota
	quantScalar
	quantBinary
)

// parseQuantization đổi chuỗi config (đã validate ở config.Load) sang enum.
// Giá trị lạ quy về none — đây chỉ là lưới an toàn, không phải nơi báo lỗi.
func parseQuantization(s string) quantizationMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "scalar":
		return quantScalar
	case "binary":
		return quantBinary
	default:
		return quantNone
	}
}

func (q quantizationMode) String() string {
	switch q {
	case quantScalar:
		return "scalar(int8)"
	case quantBinary:
		return "binary"
	default:
		return "none"
	}
}

type connection struct {
	client *qdrantapi.Client
	dim    int
	// collection cho phép test trỏ sang collection riêng — production luôn là
	// constants.QDRANT_COLLECTION.
	collection string

	// Cấu hình nén + đĩa (xem config.QdrantConfig). quant là mong muốn,
	// quantized là trạng thái THẬT của collection — chỉ bật rescore khi
	// collection thật sự nén, tránh gửi tham số vô nghĩa.
	quant          quantizationMode
	quantized      bool
	rescore        bool
	oversampling   float64
	principalIndex bool
	onDiskVectors  bool
	onDiskPayload  bool
}

func NewConnection(cfg config.IConfig) (domain.IVectorStore, error) {
	q := cfg.GetQdrant()
	if q.Host == "" {
		return nil, fmt.Errorf("qdrant: host is required")
	}
	dim := cfg.GetEmbedding().Dim
	if dim <= 0 {
		return nil, fmt.Errorf("qdrant: embedding dim must be > 0")
	}
	grpcPort := q.GrpcPort
	if grpcPort == 0 {
		grpcPort = 6334 // gRPC mặc định
	}
	client, err := qdrantapi.NewClient(&qdrantapi.Config{
		Host:   q.Host,
		Port:   grpcPort,
		APIKey: q.Token,
		// Scheme https trong file config nghĩa là TLS đã bật ở server.
		UseTLS:                 q.Scheme == "https",
		VersionCheckTimeout:    10 * time.Second,
		SkipCompatibilityCheck: false,
	})
	if err != nil {
		return nil, fmt.Errorf("qdrant: connect %s:%d: %w", q.Host, grpcPort, err)
	}
	c := &connection{
		client:         client,
		dim:            dim,
		collection:     constants.QDRANT_COLLECTION,
		quant:          parseQuantization(q.Quantization),
		rescore:        q.Rescore,
		oversampling:   q.Oversampling,
		principalIndex: q.PrincipalIndex,
		onDiskVectors:  q.OnDiskVectors,
		onDiskPayload:  q.OnDiskPayload,
	}
	if c.oversampling <= 0 {
		c.oversampling = 2.0
	}

	// Lifetime-init: collection phải tồn tại đúng dim trước khi worker chạy —
	// fail-fast giống postgres/S3 để không boot trong trạng thái embed mù.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.ensureCollection(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}

	logs.Infow("qdrant ready", "collection", c.collection, "dim", dim,
		"quantization", c.quant.String(), "quantized", c.quantized,
		"rescore", c.rescore, "on_disk_vectors", c.onDiskVectors)
	return c, nil
}

// ensureCollection: chưa có thì tạo với cấu hình cho kho lớn (dim đúng,
// quantization, on_disk, HNSW payload-aware), có rồi thì verify dim + bật
// quantization nếu config yêu cầu (best-effort), rồi tạo index cho các khóa
// phân quyền để search gắn filter đều ăn index.
func (c *connection) ensureCollection(ctx context.Context) error {
	exists, err := c.client.CollectionExists(ctx, c.collection)
	if err != nil {
		return fmt.Errorf("qdrant: kiểm tra collection %s: %w", c.collection, err)
	}
	if !exists {
		if err := c.createCollection(ctx); err != nil {
			return fmt.Errorf("qdrant: create collection %s: %w", c.collection, err)
		}
		c.quantized = c.quant != quantNone
		logs.Infow("qdrant collection created", "collection", c.collection,
			"dim", c.dim, "quantization", c.quant.String(),
			"on_disk_vectors", c.onDiskVectors, "on_disk_payload", c.onDiskPayload)
		c.ensurePayloadIndexes(ctx, nil)
		return nil
	}

	info, err := c.client.GetCollectionInfo(ctx, c.collection)
	if err != nil {
		return fmt.Errorf("qdrant: đọc info collection %s: %w", c.collection, err)
	}
	// Collection cũ (single unnamed vector) không tương thích named vectors —
	// drop & recreate để chuyển sang dual embedding. Mất data cũ nhưng rebuild
	// vectors qua admin endpoint sẽ khôi phục.
	vc := info.GetConfig().GetParams().GetVectorsConfig()
	if vc == nil {
		logs.Warnw("qdrant: không đọc được vectors config, recreate", "collection", c.collection)
		if err := c.client.DeleteCollection(ctx, c.collection); err != nil {
			return fmt.Errorf("qdrant: xóa collection %s: %w", c.collection, err)
		}
		return c.createCollection(ctx)
	}
	// unnamed vector (cũ) → Params != nil
	if vc.GetParams() != nil {
		logs.Warnw("qdrant: collection cũ (unnamed vector), recreate cho dual embedding", "collection", c.collection)
		if err := c.client.DeleteCollection(ctx, c.collection); err != nil {
			return fmt.Errorf("qdrant: xóa collection cũ %s: %w", c.collection, err)
		}
		return c.createCollection(ctx)
	}
	named := vc.GetParamsMap()
	if named == nil || named.Map == nil || named.Map["content"] == nil {
		logs.Warnw("qdrant: collection thiếu named vector content, recreate", "collection", c.collection)
		if err := c.client.DeleteCollection(ctx, c.collection); err != nil {
			return fmt.Errorf("qdrant: xóa collection %s: %w", c.collection, err)
		}
		return c.createCollection(ctx)
	}
	// Getter protobuf nil-safe nên chain dài vẫn an toàn khi shape thiếu.
	if got := int(named.Map["content"].Size); got != c.dim {
		return fmt.Errorf("qdrant: collection %s có dim %d, config expects %d",
			c.collection, got, c.dim)
	}
	c.quantized = info.GetConfig().GetQuantizationConfig() != nil
	if !c.quantized {
		c.enableQuantization(ctx)
	}
	// on_disk của vector là thuộc tính lúc tạo — nói rõ trong log thay vì
	// im lặng để người vận hành biết vì sao RAM vẫn cao.
	if c.onDiskVectors {
		if mem := info.GetConfig().GetParams().GetVectorsConfig().GetParams().GetMemory(); mem != qdrantapi.Memory_Cold && mem != qdrantapi.Memory_Cached {
			logs.Warnw("qdrant: on_disk_vectors chỉ áp được lúc tạo collection — collection hiện có vẫn giữ vector trong RAM",
				"collection", c.collection)
		}
	}
	c.ensurePayloadIndexes(ctx, info.GetPayloadSchema())
	return nil
}

// createCollection dựng collection mới với cấu hình đủ cho kho lớn:
//   - quantization (nếu config bật) — nén vector 4x/32x;
//   - on_disk vector/payload (nếu config bật) — RAM không phụ thuộc số point;
//   - HNSW payload_m > 0 — filter user_id vẫn đi HNSW thay vì full-scan;
//   - optimizer ngưỡng cao hơn mặc định để ít segment, ít memmap hơn.
func (c *connection) createCollection(ctx context.Context) error {
	vectors := &qdrantapi.VectorParams{
		Size:     uint64(c.dim),
		Distance: qdrantapi.Distance_Cosine,
	}
	if c.onDiskVectors {
		// Cold = không nạp sẵn vào RAM, đọc qua disk cache; Pinned không hỗ
		// trợ cho dense vector.
		vectors.Memory = ptrMemory(qdrantapi.Memory_Cold)
	}
	req := &qdrantapi.CreateCollection{
		CollectionName: c.collection,
		VectorsConfig: qdrantapi.NewVectorsConfigMap(map[string]*qdrantapi.VectorParams{
			"content": vectors,
			"title":   {Size: uint64(c.dim), Distance: qdrantapi.Distance_Cosine},
		}),
		HnswConfig: &qdrantapi.HnswConfigDiff{
			M:                 ptrUint64(16),
			EfConstruct:       ptrUint64(100),
			PayloadM:          ptrUint64(16),
			FullScanThreshold: ptrUint64(20000),
		},
		OptimizersConfig: &qdrantapi.OptimizersConfigDiff{
			DefaultSegmentNumber: ptrUint64(2),
			MemmapThreshold:      ptrUint64(20000),
			IndexingThreshold:    ptrUint64(20000),
		},
		QuantizationConfig: c.quantizationConfig(),
	}
	if c.onDiskPayload {
		req.Payload = &qdrantapi.PayloadStorageParams{Memory: ptrMemory(qdrantapi.Memory_Cold)}
	}
	return c.client.CreateCollection(ctx, req)
}

// enableQuantization bật nén cho collection ĐÃ CÓ. Best-effort: đây là tối ưu
// bộ nhớ, lỗi không được làm worker không boot. Có timeout riêng vì Qdrant có
// thể chờ optimization hiện tại xong trước khi áp cấu hình mới.
func (c *connection) enableQuantization(ctx context.Context) {
	diff := c.quantizationDiff()
	if diff == nil {
		return
	}
	uctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := c.client.UpdateCollection(uctx, &qdrantapi.UpdateCollection{
		CollectionName:     c.collection,
		QuantizationConfig: diff,
	}); err != nil {
		logs.Warnw("qdrant: không bật được quantization cho collection có sẵn (bỏ qua)",
			"collection", c.collection, "mode", c.quant.String(), "error", err)
		return
	}
	c.quantized = true
	logs.Infow("qdrant: đã bật quantization cho collection có sẵn",
		"collection", c.collection, "mode", c.quant.String())
}

// quantizationConfig dùng lúc tạo collection.
func (c *connection) quantizationConfig() *qdrantapi.QuantizationConfig {
	switch c.quant {
	case quantScalar:
		return &qdrantapi.QuantizationConfig{
			Quantization: &qdrantapi.QuantizationConfig_Scalar{Scalar: scalarQuantization()},
		}
	case quantBinary:
		return &qdrantapi.QuantizationConfig{
			Quantization: &qdrantapi.QuantizationConfig_Binary{Binary: binaryQuantization()},
		}
	default:
		return nil
	}
}

// quantizationDiff dùng lúc UpdateCollection (collection đã tồn tại).
func (c *connection) quantizationDiff() *qdrantapi.QuantizationConfigDiff {
	switch c.quant {
	case quantScalar:
		return &qdrantapi.QuantizationConfigDiff{
			Quantization: &qdrantapi.QuantizationConfigDiff_Scalar{Scalar: scalarQuantization()},
		}
	case quantBinary:
		return &qdrantapi.QuantizationConfigDiff{
			Quantization: &qdrantapi.QuantizationConfigDiff_Binary{Binary: binaryQuantization()},
		}
	default:
		return nil
	}
}

// scalarQuantization: int8 + quantile 0.99 (bỏ 1% đuôi để không mất độ
// chính xác vì outlier), giữ vector nén trong RAM (4x nhỏ hơn float32).
func scalarQuantization() *qdrantapi.ScalarQuantization {
	return &qdrantapi.ScalarQuantization{
		Type:     qdrantapi.QuantizationType_Int8,
		Quantile: ptrFloat32(0.99),
		Memory:   ptrMemory(qdrantapi.Memory_Pinned),
	}
}

// binaryQuantization: 1 bit/chiều (32x nhỏ hơn float32). Bắt buộc rescore
// khi search, nếu không kết quả thô và lệch hẳn.
func binaryQuantization() *qdrantapi.BinaryQuantization {
	return &qdrantapi.BinaryQuantization{Memory: ptrMemory(qdrantapi.Memory_Pinned)}
}

// payloadIndexSpec mô tả 1 payload index cần có: field + kiểu + params đặc
// biệt (principal cho user_id, text cho full-text).
type payloadIndexSpec struct {
	field  string
	typ    qdrantapi.FieldType
	params *qdrantapi.PayloadIndexParams
}

// ensurePayloadIndexes tạo các index còn THIẾU. existing lấy từ
// CollectionInfo.PayloadSchema (nil khi vừa tạo collection) để không gọi
// create lặp lại mỗi lần boot và không nuốt lỗi "đã tồn tại".
//
// Index tạo lỗi vẫn chỉ warn: thiếu index thì filter chậm hơn, không sai kết
// quả — không đáng chặn boot.
func (c *connection) ensurePayloadIndexes(ctx context.Context, existing map[string]*qdrantapi.PayloadSchemaInfo) {
	for _, spec := range c.payloadIndexSpecs() {
		if info, ok := existing[spec.field]; ok {
			// is_principal chỉ đặt được lúc tạo index. Nói rõ để người vận
			// hành biết muốn bật phải xoá index rồi restart.
			if spec.field == "user_id" && c.principalIndex && !isPrincipalIndex(info) {
				logs.Infow("qdrant: index user_id đã tồn tại nhưng chưa principal — xoá index rồi restart để bật partition theo tenant",
					"collection", c.collection, "field", spec.field)
			}
			continue
		}
		ft := spec.typ
		req := &qdrantapi.CreateFieldIndexCollection{
			CollectionName: c.collection,
			FieldName:      spec.field,
			FieldType:      &ft,
		}
		if spec.params != nil {
			req.FieldIndexParams = spec.params
		}
		if _, err := c.client.CreateFieldIndex(ctx, req); err != nil {
			logs.Warnw("qdrant: tạo payload index thất bại (bỏ qua)",
				"trace_id", utils.TraceIDFromCtx(ctx), "field", spec.field, "error", err)
		}
	}
}

// payloadIndexSpecs: 4 khóa phân quyền kiểu integer + 1 full-text cho nửa
// keyword của hybrid search.
func (c *connection) payloadIndexSpecs() []payloadIndexSpec {
	specs := make([]payloadIndexSpec, 0, 5)
	for _, field := range []string{"article_id", "category_id", "chunk_index"} {
		specs = append(specs, payloadIndexSpec{field: field, typ: qdrantapi.FieldType_FieldTypeInteger})
	}
	specs = append(specs, payloadIndexSpec{
		field:  "user_id",
		typ:    qdrantapi.FieldType_FieldTypeInteger,
		params: c.userIDIndexParams(),
	})
	asciiFold := true
	specs = append(specs, payloadIndexSpec{
		field: "text",
		typ:   qdrantapi.FieldType_FieldTypeText,
		params: qdrantapi.NewPayloadIndexParamsText(&qdrantapi.TextIndexParams{
			Tokenizer:    qdrantapi.TokenizerType_Word,
			AsciiFolding: &asciiFold,
		}),
	})
	return specs
}

// userIDIndexParams bật is_principal (partition theo tenant) — mọi query đều
// lọc user_id nên Qdrant chỉ đọc phần dữ liệu của tenant thay vì quét cả
// collection. Index cũ đã tạo không đổi được params, xem ensurePayloadIndexes.
func (c *connection) userIDIndexParams() *qdrantapi.PayloadIndexParams {
	if !c.principalIndex {
		return nil
	}
	yes := true
	return &qdrantapi.PayloadIndexParams{
		IndexParams: &qdrantapi.PayloadIndexParams_IntegerIndexParams{
			IntegerIndexParams: &qdrantapi.IntegerIndexParams{IsPrincipal: &yes},
		},
	}
}

func isPrincipalIndex(info *qdrantapi.PayloadSchemaInfo) bool {
	return info.GetParams().GetIntegerIndexParams().GetIsPrincipal()
}

// articleFilter lọc point của đúng 1 bài — purge xóa, embed đếm guard, search
// phân quyền sau này đều dùng chung.
func articleFilter(articleID int64) *qdrantapi.Filter {
	return &qdrantapi.Filter{
		Must: []*qdrantapi.Condition{
			qdrantapi.NewMatchInt("article_id", articleID),
		},
	}
}

// articlesFilter lọc point của nhiều bài 1 lần — MatchInts gộp N match
// thành 1 điều kiện nên 1 câu delete xoá được cả danh mục thay vì N round
// trip.
func articlesFilter(articleIDs []int64) *qdrantapi.Filter {
	return &qdrantapi.Filter{
		Must: []*qdrantapi.Condition{
			qdrantapi.NewMatchInts("article_id", articleIDs...),
		},
	}
}

// UpsertPoints ghi đè point theo ID — idempotent nên embed chạy lại vẫn hội tụ.
// Nhận slice con trỏ để khỏi copy struct (nhất là map Payload) mỗi lần upsert.
func (c *connection) UpsertPoints(ctx context.Context, points []*domain.VectorPoint) error {
	if len(points) == 0 {
		return nil
	}
	wait := true
	structs := make([]*qdrantapi.PointStruct, 0, len(points))
	for i, p := range points {
		if p == nil {
			// Không bao giờ xảy ra từ embed (luôn dựng point đầy đủ) — log
			// để lộ caller nào bắn point rỗng thay vì nuốt thầm lặng.
			logs.Warnw("qdrant: bỏ qua point nil", "trace_id", utils.TraceIDFromCtx(ctx), "index", i, "total", len(points))
			continue
		}
		if len(p.Vector) != c.dim {
			return fmt.Errorf("%w: point %s có dim %d, collection expects %d", ErrBadRequest, p.ID, len(p.Vector), c.dim)
		}
		titleVec := p.TitleVector
		if len(titleVec) == 0 {
			titleVec = p.Vector
		}
		if len(titleVec) != c.dim {
			return fmt.Errorf("%w: title vector point %s có dim %d, collection expects %d", ErrBadRequest, p.ID, len(titleVec), c.dim)
		}
		structs = append(structs, &qdrantapi.PointStruct{
			Id: qdrantapi.NewIDUUID(p.ID),
			Vectors: qdrantapi.NewVectorsMap(map[string]*qdrantapi.Vector{
				"content": qdrantapi.NewVectorDense(p.Vector),
				"title":   qdrantapi.NewVectorDense(titleVec),
			}),
			Payload: qdrantapi.NewValueMap(map[string]any{
				"article_id":  p.ArticleID,
				"user_id":     p.UserID,
				"category_id": p.CategoryID,
				"chunk_index": p.ChunkIndex,
				"page_num":    p.PageNum,
				"section":     p.Section,
				"text":        p.Text,
				"keywords":    p.Keywords,
			}),
		})
	}
	if _, err := c.client.Upsert(ctx, &qdrantapi.UpsertPoints{
		CollectionName: c.collection,
		Wait:           &wait,
		Points:         structs,
	}); err != nil {
		return fmt.Errorf("qdrant: upsert %d points: %w", len(points), err)
	}
	return nil
}

// DeleteByArticle xóa toàn bộ point của 1 bài — Qdrant filter khớp 0 point
// cũng báo thành công nên 2 worker cùng dọn vẫn an toàn.
func (c *connection) DeleteByArticle(ctx context.Context, articleID int64) error {
	wait := true
	if _, err := c.client.Delete(ctx, &qdrantapi.DeletePoints{
		CollectionName: c.collection,
		Wait:           &wait,
		Points:         qdrantapi.NewPointsSelectorFilter(articleFilter(articleID)),
	}); err != nil {
		return fmt.Errorf("qdrant: delete points của article %d: %w", articleID, err)
	}
	return nil
}

// deleteBatchSize trần số article_id trong 1 filter — MatchInts nhận mảng
// dài không giới hạn nhưng càng nhiều điều kiện Qdrant càng chậm khi quét
// index; chia đợt giữ cây 1 lần gọi mạng, giống chunkInsertBatch bên Postgres.
const deleteBatchSize = 500

// DeleteByArticles xoá point của nhiều bài, chia đợt 500 id/lần gọi.
// Không có transaction giữa các đợt: lỗi giữa chừng thì đợt trước đã mất
// point — nhưng cả lệnh đều idempotent (lọc khớp 0 point vẫn thành công)
// nên chạy lại là hội tụ, đúng nguyên tắc của purge.
func (c *connection) DeleteByArticles(ctx context.Context, articleIDs []int64) error {
	if len(articleIDs) == 0 {
		return nil
	}
	for _, batch := range utils.Batch(articleIDs, deleteBatchSize) {
		wait := true
		if _, err := c.client.Delete(ctx, &qdrantapi.DeletePoints{
			CollectionName: c.collection,
			Wait:           &wait,
			Points:         qdrantapi.NewPointsSelectorFilter(articlesFilter(batch)),
		}); err != nil {
			return fmt.Errorf("qdrant: delete points của %d article: %w", len(batch), err)
		}
	}
	return nil
}

// searchFilter dựng filter quyền query — user_id luôn có, category/article
// chỉ gắn khi drill-down để filter gọn đúng phạm vi.
func searchFilter(f domain.SearchFilter) *qdrantapi.Filter {
	return &qdrantapi.Filter{Must: permissionMusts(f)}
}

// permissionMusts list điều kiện phân quyền dùng chung cho Search (dense) và
// SearchText (keyword) — 2 nửa hybrid search phải cùng phạm vi, không được
// lệch (kẻo text search lộ bài người khác).
func permissionMusts(f domain.SearchFilter) []*qdrantapi.Condition {
	must := []*qdrantapi.Condition{
		qdrantapi.NewMatchInt("user_id", f.UserID),
	}
	if len(f.CategoryIDs) > 0 {
		must = append(must, qdrantapi.NewMatchInts("category_id", f.CategoryIDs...))
	}
	if f.ArticleID > 0 {
		must = append(must, qdrantapi.NewMatchInt("article_id", f.ArticleID))
	}
	return must
}

// Search trả top chunk gần vector nhất trong phạm vi filter — agent RAG gọi
// qua service search. Sai dim là lỗi client (permanent); threshold/limit chỉ
// là tham số query nên server lỗi gì cũng trả về để caller quyết.
// queryNamed chạy 1 search trên named vector (content/title) và trả hits.
func (c *connection) queryNamed(ctx context.Context, named string, vector []float32, filter domain.SearchFilter, limit int, scoreThreshold float32) ([]*domain.ScoredChunk, error) {
	req := &qdrantapi.QueryPoints{
		CollectionName: c.collection,
		Using:          &named,
		Query:          qdrantapi.NewQuery(vector...),
		Filter:         searchFilter(filter),
		Limit:          ptrUint64(uint64(limit)),
		WithPayload:    qdrantapi.NewWithPayload(true),
	}
	if scoreThreshold > 0 {
		req.ScoreThreshold = &scoreThreshold
	}
	req.Params = c.searchParams()
	res, err := c.client.Query(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("qdrant: search %s: %w", named, err)
	}
	hits := make([]*domain.ScoredChunk, 0, len(res))
	for _, p := range res {
		if p == nil {
			continue
		}
		payload := qdrantapi.ValueMapToMap(p.GetPayload())
		hits = append(hits, &domain.ScoredChunk{
			PointID:    p.GetId().GetUuid(),
			Score:      p.GetScore(),
			ArticleID:  asInt64(payload["article_id"]),
			ChunkIndex: int(asInt64(payload["chunk_index"])),
			PageNum:    int(asInt64(payload["page_num"])),
			Section:    asString(payload["section"]),
			Text:       asString(payload["text"]),
		})
	}
	return hits, nil
}

// Search chạy dual embedding: search cả "content" và "title" named vector với
// cùng query vector, rồi fuse bằng RRF (k=60) để chunk khớp cả nội dung lẫn
// tiêu đề bài được ưu tiên. Score trả về là RRF score (không phải cosine).
func (c *connection) Search(ctx context.Context, vector []float32, filter domain.SearchFilter, limit int, scoreThreshold float32) ([]*domain.ScoredChunk, error) {
	if len(vector) != c.dim {
		return nil, fmt.Errorf("%w: vector query có dim %d, collection expects %d", ErrBadRequest, len(vector), c.dim)
	}
	if limit <= 0 {
		limit = constants.SEARCH_DEFAULT_LIMIT
	}
	contentHits, err := c.queryNamed(ctx, "content", vector, filter, limit, scoreThreshold)
	if err != nil {
		return nil, err
	}
	titleHits, err := c.queryNamed(ctx, "title", vector, filter, limit, 0)
	if err != nil {
		// Title search lỗi không nên làm fail cả query — degrade về content-only.
		logs.Warnw("qdrant: title search lỗi, fallback content-only", "error", err)
		titleHits = nil
	}
	// Title score thô (cosine) để gate: chunk chỉ xuất hiện qua title phải có
	// title score >= threshold mới được thêm (tránh title-only yếu lọt qua).
	titleScore := make(map[string]float32, len(titleHits))
	for _, h := range titleHits {
		titleScore[h.PointID] = h.Score
	}

	// RRF fusion: score = sum 1/(k+rank) cho mỗi nửa. k=60 là hằng số chuẩn.
	const rrfK = 60.0
	type acc struct {
		hit   *domain.ScoredChunk
		score float64
	}
	byID := make(map[string]*acc)
	add := func(hits []*domain.ScoredChunk, gate bool) {
		for rank, h := range hits {
			if gate && scoreThreshold > 0 && titleScore[h.PointID] < scoreThreshold {
				continue
			}
			a, ok := byID[h.PointID]
			if !ok {
				hc := *h
				a = &acc{hit: &hc}
				byID[h.PointID] = a
			}
			a.score += 1.0 / (rrfK + float64(rank+1))
		}
	}
	add(contentHits, false)
	add(titleHits, true)

	out := make([]*domain.ScoredChunk, 0, len(byID))
	for _, a := range byID {
		a.hit.Score = float32(a.score)
		out = append(out, a.hit)
	}
	// Sort by RRF score desc.
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// searchParams trả tham số rescore cho QueryPoints, nil khi không cần.
// Tách khỏi Search để test được (Search cần Qdrant thật).
func (c *connection) searchParams() *qdrantapi.SearchParams {
	if !c.rescore || !c.quantized {
		return nil
	}
	return &qdrantapi.SearchParams{
		Quantization: &qdrantapi.QuantizationSearchParams{
			Rescore:      ptrBool(true),
			Oversampling: ptrFloat64(c.oversampling),
		},
	}
}

// ptrUint64 bọc số thành con trỏ cho field oneof của proto — go-client không
// có helper sẵn cho Limit nên viết tay 1 dòng.
func ptrUint64(n uint64) *uint64 { return &n }

// ptrUint32 như trên cho ScrollPoints.Limit.
func ptrUint32(n uint32) *uint32 { return &n }

func ptrBool(b bool) *bool { return &b }

func ptrFloat32(f float32) *float32 { return &f }

func ptrFloat64(f float64) *float64 { return &f }

// ptrMemory chọn chỗ đặt dữ liệu (Cold = đĩa, Pinned = RAM) cho các field
// proto dùng enum Memory thay cho cờ on_disk cũ.
func ptrMemory(m qdrantapi.Memory) *qdrantapi.Memory { return &m }

// SearchText lọc chunk theo từ khóa trong payload text (full-text MatchText)
// cùng phạm vi filter quyền — nửa keyword của hybrid search. MatchText của
// Qdrant là "có ít nhất một từ của query" và Scroll trả theo thứ tự nội bộ của
// segment: kết quả là TẬP KHỚP, không phải danh sách xếp hạng, Score để 0.
// Bên gọi xin dư rồi tự xếp hạng (xem domain.IVectorStore). Query rỗng trả nil
// (service không gọi khi query rỗng, đây là guard cho agent tool gọi thẳng).
func (c *connection) SearchText(ctx context.Context, query string, filter domain.SearchFilter, limit int) ([]*domain.ScoredChunk, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = constants.SEARCH_DEFAULT_LIMIT
	}
	must := append(permissionMusts(filter), qdrantapi.NewMatchText("text", query))
	points, err := c.client.Scroll(ctx, &qdrantapi.ScrollPoints{
		CollectionName: c.collection,
		Filter:         &qdrantapi.Filter{Must: must},
		Limit:          ptrUint32(uint32(limit)),
		WithPayload:    qdrantapi.NewWithPayload(true),
	})
	if err != nil {
		return nil, fmt.Errorf("qdrant: text search: %w", err)
	}
	hits := make([]*domain.ScoredChunk, 0, len(points))
	for _, p := range points {
		if p == nil {
			continue
		}
		payload := qdrantapi.ValueMapToMap(p.GetPayload())
		hits = append(hits, &domain.ScoredChunk{
			PointID:    p.GetId().GetUuid(),
			ArticleID:  asInt64(payload["article_id"]),
			ChunkIndex: int(asInt64(payload["chunk_index"])),
			PageNum:    int(asInt64(payload["page_num"])),
			Section:    asString(payload["section"]),
			Text:       asString(payload["text"]),
		})
	}
	return hits, nil
}

// ScrollNeighbors đọc chunk của 1 bài quanh center (center-window đến
// center+window) — tool read_context. Lọc Range ngay trên Qdrant (field
// chunk_index có index) nên chỉ đúng 2*window+1 point về memory, không scroll
// cả bài. Trả theo ChunkIndex tăng dần, Score = 0.
func (c *connection) ScrollNeighbors(ctx context.Context, filter domain.SearchFilter, center, window int) ([]*domain.ScoredChunk, error) {
	if filter.ArticleID <= 0 {
		return nil, fmt.Errorf("%w: ScrollNeighbors cần article_id", ErrBadRequest)
	}
	if window < 0 {
		window = 0
	}
	lo, hi := float64(center-window), float64(center+window)
	must := append(permissionMusts(filter), qdrantapi.NewRange("chunk_index", &qdrantapi.Range{
		Gte: &lo,
		Lte: &hi,
	}))
	points, err := c.client.Scroll(ctx, &qdrantapi.ScrollPoints{
		CollectionName: c.collection,
		Filter:         &qdrantapi.Filter{Must: must},
		Limit:          ptrUint32(uint32(2*window + 1)),
		WithPayload:    qdrantapi.NewWithPayload(true),
	})
	if err != nil {
		return nil, fmt.Errorf("qdrant: scroll neighbors: %w", err)
	}
	hits := make([]*domain.ScoredChunk, 0, 2*window+1)
	for _, p := range points {
		if p == nil {
			continue
		}
		payload := qdrantapi.ValueMapToMap(p.GetPayload())
		hits = append(hits, &domain.ScoredChunk{
			PointID:    p.GetId().GetUuid(),
			ArticleID:  asInt64(payload["article_id"]),
			ChunkIndex: int(asInt64(payload["chunk_index"])),
			PageNum:    int(asInt64(payload["page_num"])),
			Section:    asString(payload["section"]),
			Text:       asString(payload["text"]),
		})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].ChunkIndex < hits[j].ChunkIndex })
	return hits, nil
}

// readArticleMaxLimit trần số chunk 1 lượt scroll của read_article — 20 chunk
// ~20k ký tự (~5-6k token) là an toàn cho 1 context window, file dài thì LLM
// gọi trang kế tiếp.
const readArticleMaxLimit = 20

// ScrollRange đọc 1 lát chunk của 1 bài trong [from, from+limit) — tool
// read_article cho agent đọc trực tiếp file theo trang. Kẹp Range hai đầu
// (chunk_index liên tục 0..N-1 lúc embed) thay vì chỉ gte: Scroll trả theo
// ID point, gte + Limit có thể bắt được chunk 45-50 rồi trang sau nhảy mất
// 20-44. Sort lại theo ChunkIndex để trang ra đúng thứ tự đọc.
func (c *connection) ScrollRange(ctx context.Context, filter domain.SearchFilter, from, limit int) ([]*domain.ScoredChunk, error) {
	if filter.ArticleID <= 0 {
		return nil, fmt.Errorf("%w: ScrollRange cần article_id", ErrBadRequest)
	}
	if from < 0 {
		from = 0
	}
	if limit <= 0 || limit > readArticleMaxLimit {
		limit = readArticleMaxLimit
	}
	lo, hi := float64(from), float64(from+limit-1)
	must := append(permissionMusts(filter), qdrantapi.NewRange("chunk_index", &qdrantapi.Range{
		Gte: &lo,
		Lte: &hi,
	}))
	points, err := c.client.Scroll(ctx, &qdrantapi.ScrollPoints{
		CollectionName: c.collection,
		Filter:         &qdrantapi.Filter{Must: must},
		Limit:          ptrUint32(uint32(limit)),
		WithPayload:    qdrantapi.NewWithPayload(true),
	})
	if err != nil {
		return nil, fmt.Errorf("qdrant: scroll range: %w", err)
	}
	hits := make([]*domain.ScoredChunk, 0, len(points))
	for _, p := range points {
		if p == nil {
			continue
		}
		payload := qdrantapi.ValueMapToMap(p.GetPayload())
		hits = append(hits, &domain.ScoredChunk{
			PointID:    p.GetId().GetUuid(),
			ArticleID:  asInt64(payload["article_id"]),
			ChunkIndex: int(asInt64(payload["chunk_index"])),
			PageNum:    int(asInt64(payload["page_num"])),
			Section:    asString(payload["section"]),
			Text:       asString(payload["text"]),
		})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].ChunkIndex < hits[j].ChunkIndex })
	return hits, nil
}

// listSectionsLimit trần point scroll khi liệt kê mục — bài dài nhất cũng chỉ
// vài trăm chunk, 500 là dư mà 1 lượt Scroll vẫn nhẹ (chỉ đọc payload).
const listSectionsLimit = 500

// ListSections trả các mục khác nhau của 1 bài theo thứ tự chunk, mỗi mục
// kèm chunk bắt đầu (tọa độ) — ground-truth đếm/liệt kê Điều + bản đồ để
// read_article nhảy vùng. Mục rỗng (chunk đầu bài chưa có heading) bỏ qua.
func (c *connection) ListSections(ctx context.Context, filter domain.SearchFilter) ([]domain.SectionSpan, error) {
	if filter.ArticleID <= 0 {
		return nil, fmt.Errorf("%w: ListSections cần article_id", ErrBadRequest)
	}
	points, err := c.client.Scroll(ctx, &qdrantapi.ScrollPoints{
		CollectionName: c.collection,
		Filter:         searchFilter(filter),
		Limit:          ptrUint32(listSectionsLimit),
		WithPayload:    qdrantapi.NewWithPayload(true),
	})
	if err != nil {
		return nil, fmt.Errorf("qdrant: list sections: %w", err)
	}
	type ranked struct {
		index   int
		section string
	}
	ranked_ := make([]ranked, 0, len(points))
	for _, p := range points {
		if p == nil {
			continue
		}
		payload := qdrantapi.ValueMapToMap(p.GetPayload())
		if sec := asString(payload["section"]); sec != "" {
			ranked_ = append(ranked_, ranked{index: int(asInt64(payload["chunk_index"])), section: sec})
		}
	}
	sort.Slice(ranked_, func(i, j int) bool { return ranked_[i].index < ranked_[j].index })
	out := make([]domain.SectionSpan, 0, len(ranked_))
	seen := make(map[string]bool, len(ranked_))
	for _, r := range ranked_ {
		// Giữ lần xuất hiện đầu (From nhỏ nhất theo thứ tự chunk) — mục lặp
		// lại giữa trang thì bản đồ vẫn chỉ 1 tọa độ.
		if seen[r.section] {
			continue
		}
		seen[r.section] = true
		out = append(out, domain.SectionSpan{Name: r.section, From: r.index})
	}
	return out, nil
}

// asInt64/asString đọc payload Qdrant đã decode — số về int64/float64, thiếu
// hoặc sai kiểu thì 0/rỗng thay vì panic cả query.
func asInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	case int:
		return int64(n)
	}
	return 0
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// CountByArticle đếm point của 1 bài — worker embed guard nhánh done-rỗng.
func (c *connection) CountByArticle(ctx context.Context, articleID int64) (int64, error) {
	exact := true
	n, err := c.client.Count(ctx, &qdrantapi.CountPoints{
		CollectionName: c.collection,
		Filter:         articleFilter(articleID),
		Exact:          &exact,
	})
	if err != nil {
		return 0, fmt.Errorf("qdrant: count points của article %d: %w", articleID, err)
	}
	return int64(n), nil
}

// CountByFilter đếm point khớp filter phân quyền — dùng chung searchFilter
// (permissionMusts) với Search/SearchText để không lệch phạm vi. read_article
// dùng hàm này thay CountByArticle để total_chunk không lộ bài ngoài scope.
func (c *connection) CountByFilter(ctx context.Context, filter domain.SearchFilter) (int64, error) {
	exact := true
	n, err := c.client.Count(ctx, &qdrantapi.CountPoints{
		CollectionName: c.collection,
		Filter:         searchFilter(filter),
		Exact:          &exact,
	})
	if err != nil {
		return 0, fmt.Errorf("qdrant: count points theo filter: %w", err)
	}
	return int64(n), nil
}

// CountCollection đếm mọi point trong collection, không filter — xem hợp đồng
// domain.IVectorStore. Filter nil nghĩa là "cả collection"; Exact vẫn bật vì
// con số này dùng để so với tổng chunk trong DB, estimate của Qdrant làm phép
// so sánh thành nhiễu.
func (c *connection) CountCollection(ctx context.Context) (int64, error) {
	exact := true
	n, err := c.client.Count(ctx, &qdrantapi.CountPoints{
		CollectionName: c.collection,
		Exact:          &exact,
	})
	if err != nil {
		return 0, fmt.Errorf("qdrant: count toàn bộ collection %s: %w", c.collection, err)
	}
	return int64(n), nil
}

func (c *connection) IsPermanentError(err error) bool { return IsPermanent(err) }

// ErrBadRequest báo request sai từ phía client (sai dim) — retry vô ích.
var ErrBadRequest = errors.New("qdrant: bad request")

// IsPermanent báo lỗi Qdrant có retry cũng vậy không: request sai (dim,
// filter, auth...) thì failed luôn; nghẽn (ResourceExhausted) và mất kết nối
// (Unavailable) là transient. Service gặp lỗi này thì failed luôn thay vì
// backoff 10 lần.
func IsPermanent(err error) bool {
	if errors.Is(err, ErrBadRequest) {
		return true
	}
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	switch st.Code() {
	case codes.InvalidArgument, codes.NotFound, codes.AlreadyExists,
		codes.PermissionDenied, codes.Unauthenticated, codes.FailedPrecondition,
		codes.OutOfRange, codes.Unimplemented:
		return true
	}
	return false
}
