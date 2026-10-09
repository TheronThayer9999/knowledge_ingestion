package services

import (
	"context"
	"net/http"

	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/domain"
)

// IArticleAdminService vận hành hàng đợi tri thức — HTTP và gRPC cùng gọi,
// worker không đụng. Mọi nghiệp vụ nặng vẫn do worker làm, service này chỉ
// đưa bài về pending để worker hốt lại.
type IArticleAdminService interface {
	// RebuildVectors đưa bài về hàng đợi chunk→embed từ đầu (Qdrant mất
	// collection, chuyển cụm mới, vector nghi hỏng). Thứ tự vector-trước-
	// DB-sau: xóa vector + chunk cũ rồi mới reset status để worker không
	// chen ngang làm dở. Lỗi giữa chừng thì fail rõ — gọi lại là hội tụ vì
	// mọi bước idempotent.
	RebuildVectors(ctx context.Context, dto *dtos.RebuildVectorsRequest) dtos.Result[*dtos.RebuildVectorsResponse]
}

type articleAdminService struct {
	articleRepo domain.IArticleRepository
	chunkRepo   domain.IArticleChunkRepository
	vectors     domain.IVectorStore
	currentUser middlewares.ICurrentUser
}

func NewArticleAdminService(articleRepo domain.IArticleRepository, chunkRepo domain.IArticleChunkRepository, vectors domain.IVectorStore, currentUser middlewares.ICurrentUser) IArticleAdminService {
	return &articleAdminService{articleRepo: articleRepo, chunkRepo: chunkRepo, vectors: vectors, currentUser: currentUser}
}

// RebuildVectors resolve scope (đúng 1 trong article_ids/category_id, chỉ bài
// của chính mình) rồi dọn vector + chunk cũ từng bài, cuối cùng reset status
// hàng loạt trong 1 câu UPDATE. Scope rỗng thì 400 để không reset nhầm all.
func (s *articleAdminService) RebuildVectors(ctx context.Context, dto *dtos.RebuildVectorsRequest) dtos.Result[*dtos.RebuildVectorsResponse] {
	// Route đã qua auth middleware nên user_id chắc chắn có trong ctx.
	userID, _ := s.currentUser.UserID(ctx)
	ids := dto.ArticleIDs
	if len(ids) == 0 {
		if dto.CategoryID <= 0 {
			return dtos.Fail[*dtos.RebuildVectorsResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "chỉ rõ article_ids hoặc category_id"))
		}
		var err error
		ids, err = s.articleRepo.IDsByCategory(ctx, dto.CategoryID, userID)
		if err != nil {
			return dtos.Fail[*dtos.RebuildVectorsResponse](err)
		}
		if len(ids) == 0 {
			return dtos.Fail[*dtos.RebuildVectorsResponse](errors.NewCustomHttpError(http.StatusNotFound, http.StatusNotFound, "danh mục không có bài viết"))
		}
	}
	// Dọn vector + chunk cũ trước khi reset — chunk mới sinh PointID mới nên
	// giữ point/chunk cũ lại thành orphan mà purge không thấy.
	for _, id := range ids {
		if err := s.vectors.DeleteByArticle(ctx, id); err != nil {
			return dtos.Fail[*dtos.RebuildVectorsResponse](err)
		}
		if err := s.chunkRepo.DeleteByArticleID(ctx, id); err != nil {
			return dtos.Fail[*dtos.RebuildVectorsResponse](err)
		}
	}
	n, err := s.articleRepo.ResetQueue(ctx, ids, userID)
	if err != nil {
		return dtos.Fail[*dtos.RebuildVectorsResponse](err)
	}
	return dtos.Ok(&dtos.RebuildVectorsResponse{ResetArticles: n, ArticleIDs: ids})
}
