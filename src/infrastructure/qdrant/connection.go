package qdrant

import (
	"context"
	"errors"
	"fmt"
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

type connection struct {
	client *qdrantapi.Client
	dim    int
	// collection cho phép test trỏ sang collection riêng — production luôn là
	// constants.QDRANT_COLLECTION.
	collection string
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
	c := &connection{client: client, dim: dim, collection: constants.QDRANT_COLLECTION}

	// Lifetime-init: collection phải tồn tại đúng dim trước khi worker chạy —
	// fail-fast giống postgres/S3 để không boot trong trạng thái embed mù.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.ensureCollection(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}

	logs.Infow("qdrant ready", "collection", c.collection, "dim", dim)
	return c, nil
}

// ensureCollection: chưa có thì tạo Cosine đúng dim, có rồi thì verify dim
// (sai dim là config lệch với model embed — fail-fast thay vì upsert lỗi hàng
// loạt), rồi tạo index cho 3 khóa phân quyền để search gắn filter đều ăn index.
func (c *connection) ensureCollection(ctx context.Context) error {
	exists, err := c.client.CollectionExists(ctx, c.collection)
	if err != nil {
		return fmt.Errorf("qdrant: kiểm tra collection %s: %w", c.collection, err)
	}
	if !exists {
		if err := c.client.CreateCollection(ctx, &qdrantapi.CreateCollection{
			CollectionName: c.collection,
			VectorsConfig: qdrantapi.NewVectorsConfig(&qdrantapi.VectorParams{
				Size:     uint64(c.dim),
				Distance: qdrantapi.Distance_Cosine,
			}),
		}); err != nil {
			return fmt.Errorf("qdrant: create collection %s: %w", c.collection, err)
		}
		logs.Infow("qdrant collection created", "collection", c.collection)
	} else {
		info, err := c.client.GetCollectionInfo(ctx, c.collection)
		if err != nil {
			return fmt.Errorf("qdrant: đọc info collection %s: %w", c.collection, err)
		}
		// Getter protobuf nil-safe nên chain dài vẫn an toàn khi shape thiếu.
		if got := int(info.GetConfig().GetParams().GetVectorsConfig().GetParams().GetSize()); got != c.dim {
			return fmt.Errorf("qdrant: collection %s có dim %d, config expects %d",
				c.collection, got, c.dim)
		}
	}
	// Index payload best-effort — có rồi hay lỗi cũng không chặn boot, chỉ
	// khiến xóa/filter theo bài chậm hơn.
	for _, field := range []string{"article_id", "user_id", "category_id"} {
		ft := qdrantapi.FieldType_FieldTypeInteger
		if _, err := c.client.CreateFieldIndex(ctx, &qdrantapi.CreateFieldIndexCollection{
			CollectionName: c.collection,
			FieldName:      field,
			FieldType:      &ft,
		}); err != nil {
			// Index đã tồn tại server trả AlreadyExists — cũng chỉ warn như
			// mọi lỗi best-effort khác.
			logs.Warnw("qdrant: tạo payload index thất bại (bỏ qua)", "trace_id", utils.TraceIDFromCtx(ctx), "field", field, "error", err)
		}
	}
	// Full-text index cho nửa keyword của hybrid search (SearchText MatchText
	// field "text"). Tokenizer Word (tiếng Việt tách theo âm tiết — đủ cho
	// keyword match) + AsciiFolding (query không dấu "kinh te" vẫn trúng
	// "kinh tế") + lowercase mặc định. Best-effort như index số: collection cũ
	// chưa có thì SearchText báo lỗi, service degraded dense-only.
	ftText := qdrantapi.FieldType_FieldTypeText
	asciiFold := true
	if _, err := c.client.CreateFieldIndex(ctx, &qdrantapi.CreateFieldIndexCollection{
		CollectionName: c.collection,
		FieldName:      "text",
		FieldType:      &ftText,
		FieldIndexParams: qdrantapi.NewPayloadIndexParamsText(&qdrantapi.TextIndexParams{
			Tokenizer:    qdrantapi.TokenizerType_Word,
			AsciiFolding: &asciiFold,
		}),
	}); err != nil {
		logs.Warnw("qdrant: tạo full-text index thất bại (bỏ qua)", "trace_id", utils.TraceIDFromCtx(ctx), "field", "text", "error", err)
	}
	return nil
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
		structs = append(structs, &qdrantapi.PointStruct{
			Id:      qdrantapi.NewIDUUID(p.ID),
			Vectors: qdrantapi.NewVectors(p.Vector...),
			Payload: qdrantapi.NewValueMap(map[string]any{
				"article_id":  p.ArticleID,
				"user_id":     p.UserID,
				"category_id": p.CategoryID,
				"chunk_index": p.ChunkIndex,
				"page_num":    p.PageNum,
				"text":        p.Text,
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
	if f.CategoryID > 0 {
		must = append(must, qdrantapi.NewMatchInt("category_id", f.CategoryID))
	}
	if f.ArticleID > 0 {
		must = append(must, qdrantapi.NewMatchInt("article_id", f.ArticleID))
	}
	return must
}

// Search trả top chunk gần vector nhất trong phạm vi filter — agent RAG gọi
// qua service search. Sai dim là lỗi client (permanent); threshold/limit chỉ
// là tham số query nên server lỗi gì cũng trả về để caller quyết.
func (c *connection) Search(ctx context.Context, vector []float32, filter domain.SearchFilter, limit int, scoreThreshold float32) ([]*domain.ScoredChunk, error) {
	if len(vector) != c.dim {
		return nil, fmt.Errorf("%w: vector query có dim %d, collection expects %d", ErrBadRequest, len(vector), c.dim)
	}
	if limit <= 0 {
		limit = constants.SEARCH_DEFAULT_LIMIT
	}
	req := &qdrantapi.QueryPoints{
		CollectionName: c.collection,
		Query:          qdrantapi.NewQuery(vector...),
		Filter:         searchFilter(filter),
		Limit:          ptrUint64(uint64(limit)),
		WithPayload:    qdrantapi.NewWithPayload(true),
	}
	if scoreThreshold > 0 {
		req.ScoreThreshold = &scoreThreshold
	}
	res, err := c.client.Query(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("qdrant: search: %w", err)
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
			Text:       asString(payload["text"]),
		})
	}
	return hits, nil
}

// ptrUint64 bọc số thành con trỏ cho field oneof của proto — go-client không
// có helper sẵn cho Limit nên viết tay 1 dòng.
func ptrUint64(n uint64) *uint64 { return &n }

// ptrUint32 như trên cho ScrollPoints.Limit.
func ptrUint32(n uint32) *uint32 { return &n }

// SearchText tìm chunk khớp từ khóa trong payload text (full-text MatchText)
// cùng phạm vi filter quyền — nửa keyword của hybrid search. Dùng Scroll
// (không score) vì service fuse bằng RRF theo rank: Score trả 0, thứ tự mảng
// là rank. Query rỗng trả nil (service không gọi khi query rỗng, đây là guard
// cho agent tool gọi thẳng).
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
			Text:       asString(payload["text"]),
		})
	}
	return hits, nil
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
