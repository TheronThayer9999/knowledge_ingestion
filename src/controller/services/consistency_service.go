package services

import (
	"context"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/infrastructure/postgres"
)

// IConsistencyService kiểm tra tính toàn vẹn dữ liệu (category closure, articles,
// article_pages + phía Qdrant), admin endpoint read-only, không ghi gì.
type IConsistencyService interface {
	Check(ctx context.Context) dtos.Result[*dtos.ConsistencyCheckResponse]
}

type consistencyService struct {
	db postgres.IDB
	// reconcile dùng đúng phép quét của worker để kiểm phía vector — một nguồn
	// sự thật cho câu "bài done nhưng mất point", không hai bản sao lệch nhau.
	reconcile IIndexReconcileService
}

func NewConsistencyService(db postgres.IDB, reconcile IIndexReconcileService) IConsistencyService {
	return &consistencyService{db: db, reconcile: reconcile}
}

func (s *consistencyService) Check(ctx context.Context) dtos.Result[*dtos.ConsistencyCheckResponse] {
	gdb := s.db.GetDB()
	// GhostArticleIDs khởi tạo rỗng để JSON ra [] chứ không null — client đọc
	// mảng không cần đoán "null là hết hay là chưa kiểm".
	res := &dtos.ConsistencyCheckResponse{GhostArticleIDs: []int64{}}

	queries := []struct {
		sql string
		dst *int64
	}{
		{`SELECT COUNT(*) FROM category_closure cc
		  WHERE NOT EXISTS (SELECT 1 FROM categories c WHERE c.id = cc.ancestor_id AND c.deleted_at IS NULL)
		     OR NOT EXISTS (SELECT 1 FROM categories c WHERE c.id = cc.descendant_id AND c.deleted_at IS NULL)`,
			&res.OrphanClosureRows},
		{`SELECT COUNT(*) FROM categories c
		  WHERE c.deleted_at IS NULL
		    AND NOT EXISTS (SELECT 1 FROM category_closure cc
		                    WHERE cc.ancestor_id = c.id AND cc.descendant_id = c.id AND cc.depth = 0)`,
			&res.MissingSelfRows},
		{`SELECT COUNT(*) FROM category_closure WHERE ancestor_id = descendant_id AND depth > 0`,
			&res.CycleIndicators},
		{`SELECT COUNT(*) FROM articles a
		  WHERE a.deleted_at IS NULL
		    AND NOT EXISTS (SELECT 1 FROM categories c WHERE c.id = a.category_id)`,
			&res.OrphanArticles},
		{`SELECT COUNT(*) FROM article_pages ap
		  WHERE NOT EXISTS (SELECT 1 FROM articles a WHERE a.id = ap.article_id)`,
			&res.OrphanArticlePages},
	}
	for _, q := range queries {
		var n int64
		if err := gdb.WithContext(ctx).Raw(q.sql).Scan(&n).Error; err != nil {
			return dtos.Fail[*dtos.ConsistencyCheckResponse](err)
		}
		*q.dst = n
	}
	s.checkVectors(ctx, res)
	res.Healthy = res.OrphanClosureRows == 0 && res.MissingSelfRows == 0 &&
		res.CycleIndicators == 0 && res.OrphanArticles == 0 && res.OrphanArticlePages == 0 &&
		res.GhostArticles == 0 && res.VectorError == ""
	return dtos.Ok(res)
}

// checkVectors hỏi phía Qdrant qua bộ reconcile: CountCollection so với tổng
// chunk DB claim, và bài nào done mà 0 point thì ghi vào ghost_articles.
//
// Qdrant không trả lời thì chỉ đánh dấu vector_error, KHÔNG dựng cả endpoint:
// phần Postgres vẫn là kết luận có giá trị, và người đọc phải thấy rõ đám số
// vector bên trên là số 0 vô nghĩa chứ không phải "sạch".
func (s *consistencyService) checkVectors(ctx context.Context, res *dtos.ConsistencyCheckResponse) {
	gaps, err := s.reconcile.ScanGhosts(ctx)
	if err != nil {
		res.VectorError = err.Error()
		logs.Error(err, "consistency: không kiểm được phía vector")
		return
	}
	res.VectorPoints = gaps.Points
	res.ClaimedChunks = gaps.Claimed
	res.ScannedArticles = int64(gaps.Scanned)
	res.GhostArticles = int64(len(gaps.Ghosts))
	res.VectorsTruncated = gaps.Truncated
	for _, article := range gaps.Ghosts[:min(len(gaps.Ghosts), constants.GHOST_SAMPLE_SIZE)] {
		res.GhostArticleIDs = append(res.GhostArticleIDs, article.ID)
	}
}
