package services

import (
	"context"
	stderrors "errors"
	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/storage"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/domain"
	"net/http"
	"strings"

	"gorm.io/gorm"
)

type IArticleService interface {
	Create(ctx context.Context, dto *dtos.CreateArticleRequest) dtos.Result[*dtos.ArticleResponse]
	GetByID(ctx context.Context, id int64) dtos.Result[*dtos.ArticleResponse]
	List(ctx context.Context, q *dtos.ListArticlesQuery) dtos.Result[dtos.ArticleListResponse]
	Update(ctx context.Context, id int64, dto *dtos.UpdateArticleRequest) dtos.Result[*dtos.ArticleResponse]
	Delete(ctx context.Context, id int64) dtos.Result[*dtos.ArticleResponse]
}

type articleService struct {
	articleRepo  domain.IArticleRepository
	categoryRepo domain.ICategoryRepositoryImpl
	currentUser  middlewares.ICurrentUser
	storage      storage.IStorage
}

func NewArticleService(articleRepo domain.IArticleRepository, categoryRepo domain.ICategoryRepositoryImpl, currentUser middlewares.ICurrentUser, storage storage.IStorage) IArticleService {
	return &articleService{
		articleRepo:  articleRepo,
		categoryRepo: categoryRepo,
		currentUser:  currentUser,
		storage:      storage,
	}
}

// Create tạo bài viết trong một danh mục của chính mình — danh mục phải
// tồn tại VÀ cùng owner. Nguồn đúng 1 trong 2: URL là link ngoài; còn
// StorageKey là bước "confirm" luồng presign — server HeadObject verify
// object thật sự tồn tại (không tin lời client) + key phải do server sinh
// (prefix uploads/) rồi mới lưu row.
func (s *articleService) Create(ctx context.Context, dto *dtos.CreateArticleRequest) dtos.Result[*dtos.ArticleResponse] {
	// Route đã qua auth middleware nên user_id chắc chắn có trong ctx.
	userID, _ := s.currentUser.UserID(ctx)
	if _, err := s.categoryRepo.GetById(ctx, dto.CategoryID, userID); err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return dtos.Fail[*dtos.ArticleResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "danh mục không tồn tại"))
		}
		return dtos.Fail[*dtos.ArticleResponse](err)
	}
	hasURL := strings.TrimSpace(dto.URL) != ""
	hasKey := strings.TrimSpace(dto.StorageKey) != ""
	if hasURL == hasKey {
		return dtos.Fail[*dtos.ArticleResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "chỉ dùng một trong url hoặc storage_key"))
	}
	if hasURL {
		exists, err := s.articleRepo.ExistsByURL(ctx, dto.URL, userID)
		if err != nil {
			return dtos.Fail[*dtos.ArticleResponse](err)
		}
		if exists {
			return dtos.Fail[*dtos.ArticleResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "bài viết với URL này đã tồn tại"))
		}
	} else {
		if !strings.HasPrefix(dto.StorageKey, constants.UPLOAD_PREFIX) {
			return dtos.Fail[*dtos.ArticleResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "storage_key không hợp lệ"))
		}
		exists, err := s.storage.Exists(ctx, dto.StorageKey)
		if err != nil {
			return dtos.Fail[*dtos.ArticleResponse](err)
		}
		if !exists {
			return dtos.Fail[*dtos.ArticleResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "file chưa được upload lên kho"))
		}
		dup, err := s.articleRepo.ExistsByStorageKey(ctx, dto.StorageKey, userID)
		if err != nil {
			return dtos.Fail[*dtos.ArticleResponse](err)
		}
		if dup {
			return dtos.Fail[*dtos.ArticleResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "file này đã được tạo bài viết"))
		}
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

// List liệt kê bài của mình theo đúng 1 bộ lọc: category_id hoặc
// category_name (tên chính xác). Không khớp gì thì trả rỗng, không 404.
// Danh mục của người khác cũng trả rỗng, không lộ gì.
func (s *articleService) List(ctx context.Context, q *dtos.ListArticlesQuery) dtos.Result[dtos.ArticleListResponse] {
	// Route đã qua auth middleware nên user_id chắc chắn có trong ctx.
	userID, _ := s.currentUser.UserID(ctx)
	hasID := q.CategoryID > 0
	hasName := strings.TrimSpace(q.CategoryName) != ""
	if hasID == hasName {
		return dtos.Fail[dtos.ArticleListResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "chỉ dùng một trong category_id hoặc category_name"))
	}
	limit, offset := utils.NormalizePagination(q.Limit, q.Offset)
	var (
		articles []*domain.Article
		err      error
	)
	if hasID {
		articles, err = s.articleRepo.ListByCategoryID(ctx, q.CategoryID, userID, limit, offset)
	} else {
		articles, err = s.articleRepo.ListByCategoryName(ctx, strings.TrimSpace(q.CategoryName), userID, limit, offset)
	}
	if err != nil {
		return dtos.Fail[dtos.ArticleListResponse](err)
	}
	return dtos.Ok(dtos.ToArticleListResponse(articles))
}

// Update sửa từng phần — field nil giữ nguyên. Nguồn (URL/StorageKey) là
// identity nên không cho sửa. Đổi danh mục thì danh mục mới phải cùng owner.
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

// Delete xóa mềm bài viết của mình — row load đã scope owner nên không thể
// xóa hộ. gorm Delete chỉ set DeletedAt; worker janitor quét định kỳ dọn
// blob rồi xóa hẳn. Article loại URL không có blob nên worker chỉ xóa row.
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
