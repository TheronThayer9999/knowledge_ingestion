package services

import (
	"context"
	"time"

	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

// IEmbedCacheAdminService vận hành cache embed — chỉ admin/ops gọi qua HTTP.
// Stats để monitor hit rate, Purge để janitor giữ bảng không phình.
type IEmbedCacheAdminService interface {
	Stats(ctx context.Context) dtos.Result[*dtos.EmbedCacheStatsResponse]
	Purge(ctx context.Context, olderThanDays int) dtos.Result[*dtos.EmbedCachePurgeResponse]
}

type embedCacheAdminService struct {
	cache domain.EmbedCache
}

func NewEmbedCacheAdminService(cache domain.EmbedCache) IEmbedCacheAdminService {
	return &embedCacheAdminService{cache: cache}
}

func (s *embedCacheAdminService) Stats(ctx context.Context) dtos.Result[*dtos.EmbedCacheStatsResponse] {
	count, bytes, err := s.cache.Stats(ctx)
	if err != nil {
		return dtos.Fail[*dtos.EmbedCacheStatsResponse](err)
	}
	return dtos.Ok(&dtos.EmbedCacheStatsResponse{Count: count, Bytes: bytes})
}

func (s *embedCacheAdminService) Purge(ctx context.Context, olderThanDays int) dtos.Result[*dtos.EmbedCachePurgeResponse] {
	if olderThanDays <= 0 {
		olderThanDays = 30
	}
	n, err := s.cache.PurgeUnused(ctx, time.Duration(olderThanDays)*24*time.Hour)
	if err != nil {
		return dtos.Fail[*dtos.EmbedCachePurgeResponse](err)
	}
	return dtos.Ok(&dtos.EmbedCachePurgeResponse{Deleted: n})
}