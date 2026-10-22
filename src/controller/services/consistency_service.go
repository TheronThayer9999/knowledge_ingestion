package services

import (
	"context"

	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/infrastructure/postgres"
)

// IConsistencyService kiểm tra tính toàn vẹn dữ liệu (category closure, articles,
// article_pages) — admin endpoint read-only, không ghi gì.
type IConsistencyService interface {
	Check(ctx context.Context) dtos.Result[*dtos.ConsistencyCheckResponse]
}

type consistencyService struct {
	db postgres.IDB
}

func NewConsistencyService(db postgres.IDB) IConsistencyService {
	return &consistencyService{db: db}
}

func (s *consistencyService) Check(ctx context.Context) dtos.Result[*dtos.ConsistencyCheckResponse] {
	gdb := s.db.GetDB()
	res := &dtos.ConsistencyCheckResponse{}

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
	res.Healthy = res.OrphanClosureRows == 0 && res.MissingSelfRows == 0 &&
		res.CycleIndicators == 0 && res.OrphanArticles == 0 && res.OrphanArticlePages == 0
	return dtos.Ok(res)
}