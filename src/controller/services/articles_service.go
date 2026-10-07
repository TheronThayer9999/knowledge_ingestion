package services

import (
	"context"
	stderrors "errors"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/domain"
	"net/http"

	"gorm.io/gorm"
)

type IArticleService interface {
	Create(ctx context.Context, dto *dtos.CreateArticleRequest) dtos.Result[*dtos.ArticleResponse]
	GetByID(ctx context.Context, id int64) dtos.Result[*dtos.ArticleResponse]
	ListByCategoryID(ctx context.Context, categoryID int64, limit, offset int) dtos.Result[dtos.ArticleListResponse]
	ListByCategoryName(ctx context.Context, categoryName string, limit, offset int) dtos.Result[dtos.ArticleListResponse]
	Update(ctx context.Context, id int64, dto *dtos.UpdateArticleRequest) dtos.Result[*dtos.ArticleResponse]
	Delete(ctx context.Context, id int64) dtos.Result[*dtos.ArticleResponse]
}

type articleService struct {
	articleRepo  domain.IArticleRepository
	categoryRepo domain.ICategoryRepositoryImpl
	currentUser  middlewares.ICurrentUser
}

func NewArticleService(articleRepo domain.IArticleRepository, categoryRepo domain.ICategoryRepositoryImpl, currentUser middlewares.ICurrentUser) IArticleService {
	return &articleService{
		articleRepo:  articleRepo,
		categoryRepo: categoryRepo,
		currentUser:  currentUser,
	}
}

// Create tạo bài viết trong một danh mục của chính mình — danh mục phải
// tồn tại VÀ cùng owner. URL dedup theo từng user, trùng thì 400.
func (s *articleService) Create(ctx context.Context, dto *dtos.CreateArticleRequest) dtos.Result[*dtos.ArticleResponse] {
	// Route đã qua auth middleware nên user_id chắc chắn có trong ctx.
	userID, _ := s.currentUser.UserID(ctx)
	if _, err := s.categoryRepo.GetById(ctx, dto.CategoryID, userID); err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return dtos.Fail[*dtos.ArticleResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "danh mục không tồn tại"))
		}
		return dtos.Fail[*dtos.ArticleResponse](err)
	}
	exists, err := s.articleRepo.ExistsByURL(ctx, dto.URL, userID)
	if err != nil {
		return dtos.Fail[*dtos.ArticleResponse](err)
	}
	if exists {
		return dtos.Fail[*dtos.ArticleResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "bài viết với URL này đã tồn tại"))
	}
	article := dto.ToModel()
	article.UserID = userID
	if err := s.articleRepo.Create(ctx, article); err != nil {
		return dtos.Fail[*dtos.ArticleResponse](err)
	}
	return dtos.Ok(dtos.ToArticleResponse(article))
}

// GetByID trả 404 khi id không tồn tại HOẶC không thuộc owner.
func (s *articleService) GetByID(ctx context.Context, id int64) dtos.Result[*dtos.ArticleResponse] {
	// Route đã qua auth middleware nên user_id chắc chắn có trong ctx.
	userID, _ := s.currentUser.UserID(ctx)
	article, err := s.articleRepo.GetByID(ctx, id, userID)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return dtos.Fail[*dtos.ArticleResponse](errors.NewCustomHttpError(http.StatusNotFound, http.StatusNotFound, "bài viết không tồn tại"))
		}
		return dtos.Fail[*dtos.ArticleResponse](err)
	}
	return dtos.Ok(dtos.ToArticleResponse(article))
}

// ListByCategoryID liệt kê bài trong một danh mục của mình — danh mục
// của người khác trả về rỗng, không lộ gì.
func (s *articleService) ListByCategoryID(ctx context.Context, categoryID int64, limit, offset int) dtos.Result[dtos.ArticleListResponse] {
	// Route đã qua auth middleware nên user_id chắc chắn có trong ctx.
	userID, _ := s.currentUser.UserID(ctx)
	logs.Info("userId:", userID)
	limit, offset = utils.NormalizePagination(limit, offset)
	articles, err := s.articleRepo.ListByCategoryID(ctx, categoryID, userID, limit, offset)
	if err != nil {
		return dtos.Fail[dtos.ArticleListResponse](err)
	}
	return dtos.Ok(dtos.ToArticleListResponse(articles))
}

// ListByCategoryName liệt kê bài theo tên danh mục chính xác trong phạm
// vi owner — không khớp tên nào thì trả rỗng.
func (s *articleService) ListByCategoryName(ctx context.Context, categoryName string, limit, offset int) dtos.Result[dtos.ArticleListResponse] {
	// Route đã qua auth middleware nên user_id chắc chắn có trong ctx.
	userID, _ := s.currentUser.UserID(ctx)
	limit, offset = utils.NormalizePagination(limit, offset)
	articles, err := s.articleRepo.ListByCategoryName(ctx, categoryName, userID, limit, offset)
	if err != nil {
		return dtos.Fail[dtos.ArticleListResponse](err)
	}
	return dtos.Ok(dtos.ToArticleListResponse(articles))
}

// Update sửa từng phần — field nil giữ nguyên. URL là identity dedup nên
// không cho sửa. Đổi danh mục thì danh mục mới phải cùng owner.
func (s *articleService) Update(ctx context.Context, id int64, dto *dtos.UpdateArticleRequest) dtos.Result[*dtos.ArticleResponse] {
	// Route đã qua auth middleware nên user_id chắc chắn có trong ctx.
	userID, _ := s.currentUser.UserID(ctx)
	article, err := s.articleRepo.GetByID(ctx, id, userID)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return dtos.Fail[*dtos.ArticleResponse](errors.NewCustomHttpError(http.StatusNotFound, http.StatusNotFound, "bài viết không tồn tại"))
		}
		return dtos.Fail[*dtos.ArticleResponse](err)
	}
	if dto.Name != nil {
		article.Name = *dto.Name
	}
	if dto.ContentType != nil {
		article.ContentType = *dto.ContentType
	}
	if dto.Description != nil {
		article.Description = *dto.Description
	}
	if dto.CategoryID != nil {
		if _, err := s.categoryRepo.GetById(ctx, *dto.CategoryID, userID); err != nil {
			if stderrors.Is(err, gorm.ErrRecordNotFound) {
				return dtos.Fail[*dtos.ArticleResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "danh mục không tồn tại"))
			}
			return dtos.Fail[*dtos.ArticleResponse](err)
		}
		article.CategoryID = *dto.CategoryID
	}
	if err := s.articleRepo.Update(ctx, article); err != nil {
		return dtos.Fail[*dtos.ArticleResponse](err)
	}
	return dtos.Ok(dtos.ToArticleResponse(article))
}

// Delete xóa bài viết của mình — row load đã scope owner nên không thể
// xóa hộ.
func (s *articleService) Delete(ctx context.Context, id int64) dtos.Result[*dtos.ArticleResponse] {
	// Route đã qua auth middleware nên user_id chắc chắn có trong ctx.
	userID, _ := s.currentUser.UserID(ctx)
	article, err := s.articleRepo.GetByID(ctx, id, userID)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return dtos.Fail[*dtos.ArticleResponse](errors.NewCustomHttpError(http.StatusNotFound, http.StatusNotFound, "bài viết không tồn tại"))
		}
		return dtos.Fail[*dtos.ArticleResponse](err)
	}
	if err := s.articleRepo.Delete(ctx, article); err != nil {
		return dtos.Fail[*dtos.ArticleResponse](err)
	}
	return dtos.Ok(dtos.ToArticleResponse(article))
}
