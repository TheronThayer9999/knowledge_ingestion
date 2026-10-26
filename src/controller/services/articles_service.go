package services

import (
	"context"
	stderrors "errors"
	"knowledge_ingestion/src/common/chunker"
	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/storage"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/domain"
	"net/http"
	"strings"
	"time"
)

type IArticleService interface {
	Create(ctx context.Context, dto *dtos.CreateArticleRequest) dtos.Result[*dtos.ArticleResponse]
	GetByID(ctx context.Context, id int64) dtos.Result[*dtos.ArticleResponse]
	List(ctx context.Context, q *dtos.ListArticlesQuery) dtos.Result[dtos.ArticleListResponse]
	Update(ctx context.Context, id int64, dto *dtos.UpdateArticleRequest) dtos.Result[*dtos.ArticleResponse]
	Delete(ctx context.Context, id int64) dtos.Result[*dtos.ArticleResponse]
	// ListSections trả PageIndex (danh mục mục + tọa độ chunk) của 1 bài của
	// mình — P2.4 phase 1a, promote từ Qdrant ListSections, không cần LLM.
	ListSections(ctx context.Context, id int64) dtos.Result[*dtos.ArticleSectionsResponse]
	// GetTree trả cây mục theo phân cấp heading (P2.4 phase 1c) — cùng nguồn
	// PageIndex + summary đã compile, nest Chương/Phần > Mục > Điều/Bài.
	GetTree(ctx context.Context, id int64) dtos.Result[*dtos.ArticleTreeResponse]
	// ListStatuses trả trạng thái pipeline của mọi bài mình sở hữu — UI
	// poll tiến trình bằng câu nhẹ này thay vì tải lại cả danh sách.
	ListStatuses(ctx context.Context) dtos.Result[dtos.ArticleStatusListResponse]
}

type articleService struct {
	articleRepo  domain.IArticleRepository
	categoryRepo domain.ICategoryRepositoryImpl
	currentUser  middlewares.ICurrentUser
	storage      storage.IStorage
	// vectors đọc mục từ Qdrant payload — DB chunk đã trim sau embed nên
	// bản đồ section chỉ còn trong vector store (giống tool list_sections).
	vectors domain.IVectorStore
	// sectionRepo đọc summary LLM đã compile (phase 1b) để merge vào
	// PageIndex — best-effort: bài chưa compile thì summary rỗng.
	sectionRepo domain.IArticleSectionRepository
	// heartbeatRepo đọc nhịp sống worker — UI cần biết worker còn chạy để không
	// báo "đang xử lý" cho một bài thực ra đang chờ vô hạn.
	heartbeatRepo domain.IWorkerHeartbeatRepository
}

func NewArticleService(articleRepo domain.IArticleRepository, categoryRepo domain.ICategoryRepositoryImpl, currentUser middlewares.ICurrentUser, storage storage.IStorage, vectors domain.IVectorStore, sectionRepo domain.IArticleSectionRepository, heartbeatRepo domain.IWorkerHeartbeatRepository) IArticleService {
	return &articleService{
		articleRepo:   articleRepo,
		categoryRepo:  categoryRepo,
		currentUser:   currentUser,
		storage:       storage,
		vectors:       vectors,
		sectionRepo:   sectionRepo,
		heartbeatRepo: heartbeatRepo,
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
		if stderrors.Is(err, domain.ErrNotFound) {
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
	detectDocType(article)
	if err := s.articleRepo.Create(ctx, article); err != nil {
		return dtos.Fail[*dtos.ArticleResponse](err)
	}
	logArticleCreated(ctx, article, userID)
	return dtos.Ok(dtos.ToArticleResponse(article))
}

// detectDocType tự đoán loại tài liệu nếu client không khai — dùng cho template
// chunking của phase index.
func detectDocType(article *domain.Article) {
	if strings.TrimSpace(article.DocType) == "" {
		article.DocType = string(chunker.DetectType(article.ContentType, article.Name))
	}
}

// logArticleCreated nối API ↔ worker bằng CÙNG `file`: đây là dòng "file vừa vào
// kho, sắp đi qua pipeline", còn mọi dòng worker đều in `file` nên chỉ cần grep
// key là ra cả hành trình. `name` chỉ là tên người dùng đặt, không phải file.
func logArticleCreated(ctx context.Context, article *domain.Article, userID int64) {
	logs.Infow("article: tạo bài, chờ worker", "trace_id", utils.TraceIDFromCtx(ctx),
		"article_id", article.ID, "name", article.Name, "file", fileTag(article),
		"content_type", article.ContentType, "doc_type", article.DocType,
		"category_id", article.CategoryID, "user_id", userID)
}

// GetByID trả 404 khi id không tồn tại HOẶC không thuộc owner.
func (s *articleService) GetByID(ctx context.Context, id int64) dtos.Result[*dtos.ArticleResponse] {
	// Route đã qua auth middleware nên user_id chắc chắn có trong ctx.
	userID, _ := s.currentUser.UserID(ctx)
	article, err := s.articleRepo.GetByID(ctx, id, userID)
	if err != nil {
		if stderrors.Is(err, domain.ErrNotFound) {
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

// ListStatuses gom trạng thái pipeline của toàn bộ bài mình sở hữu trong 1
// câu SELECT 4 cột, kèm nhịp sống worker — UI dùng để tự cập nhật badge và
// cảnh báo "worker đã tắt" khi có bài đang chờ.
func (s *articleService) ListStatuses(ctx context.Context) dtos.Result[dtos.ArticleStatusListResponse] {
	userID, _ := s.currentUser.UserID(ctx)
	articles, err := s.articleRepo.ListStatuses(ctx, userID)
	if err != nil {
		return dtos.Fail[dtos.ArticleStatusListResponse](err)
	}
	lastSeen, seen, err := s.heartbeatRepo.LatestSeen(ctx)
	if err != nil {
		return dtos.Fail[dtos.ArticleStatusListResponse](err)
	}
	res := dtos.ToArticleStatusListResponse(articles)
	res.Worker = dtos.WorkerStatusResponse{
		Online: seen && time.Since(lastSeen) <= constants.WORKER_OFFLINE_AFTER,
	}
	if seen {
		res.Worker.LastSeenAt = &lastSeen
	}
	return dtos.Ok(res)
}

// Update sửa từng phần — field nil giữ nguyên. Nguồn (URL/StorageKey) là
// identity nên không cho sửa. Đổi danh mục thì danh mục mới phải cùng owner.
func (s *articleService) Update(ctx context.Context, id int64, dto *dtos.UpdateArticleRequest) dtos.Result[*dtos.ArticleResponse] {
	// Route đã qua auth middleware nên user_id chắc chắn có trong ctx.
	userID, _ := s.currentUser.UserID(ctx)
	article, err := s.articleRepo.GetByID(ctx, id, userID)
	if err != nil {
		if stderrors.Is(err, domain.ErrNotFound) {
			return dtos.Fail[*dtos.ArticleResponse](errors.NewCustomHttpError(http.StatusNotFound, http.StatusNotFound, "bài viết không tồn tại"))
		}
		return dtos.Fail[*dtos.ArticleResponse](err)
	}
	if dto.Name != nil {
		article.Name = *dto.Name
	}
	if dto.ContentType != nil {
		article.ContentType = *dto.ContentType
		if dto.DocType == nil {
			article.DocType = string(chunker.DetectType(article.ContentType, article.Name))
		}
	}
	if dto.DocType != nil {
		article.DocType = *dto.DocType
	}
	if dto.Description != nil {
		article.Description = *dto.Description
	}
	if dto.CategoryID != nil {
		if _, err := s.categoryRepo.GetById(ctx, *dto.CategoryID, userID); err != nil {
			if stderrors.Is(err, domain.ErrNotFound) {
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
		if stderrors.Is(err, domain.ErrNotFound) {
			return dtos.Fail[*dtos.ArticleResponse](errors.NewCustomHttpError(http.StatusNotFound, http.StatusNotFound, "bài viết không tồn tại"))
		}
		return dtos.Fail[*dtos.ArticleResponse](err)
	}
	if err := s.articleRepo.Delete(ctx, article); err != nil {
		return dtos.Fail[*dtos.ArticleResponse](err)
	}

	return dtos.Ok(dtos.ToArticleResponse(article))
}

// ListSections trả PageIndex của 1 bài — chủ sở hữu được xác minh qua repo
// trước (GetByID scope owner, bài của người khác 404 như không tồn tại), rồi
// mới đọc Qdrant với filter UserID từ token (không tin client) + ArticleID.
// Bài chưa index xong thì mục trả về rỗng kèm index_status để client tự phân
// biệt, không phải lỗi. Summary (phase 1b) đọc từ article_sections merge theo
// tên mục — bài chưa compile thì summary rỗng, không phải lỗi.
func (s *articleService) ListSections(ctx context.Context, id int64) dtos.Result[*dtos.ArticleSectionsResponse] {
	article, sections, summaries, err := s.loadSections(ctx, id)
	if err != nil {
		return dtos.Fail[*dtos.ArticleSectionsResponse](err)
	}
	return dtos.Ok(dtos.ToArticleSectionsResponse(article, sections, summaries))
}

// GetTree dựng cây mục (phase 1c) từ đúng dữ liệu ListSections — builder
// thuần trong dtos, không LLM, không thêm lượt Qdrant/DB nào.
func (s *articleService) GetTree(ctx context.Context, id int64) dtos.Result[*dtos.ArticleTreeResponse] {
	article, sections, summaries, err := s.loadSections(ctx, id)
	if err != nil {
		return dtos.Fail[*dtos.ArticleTreeResponse](err)
	}
	return dtos.Ok(dtos.BuildArticleTree(article, sections, summaries))
}

// loadSections là phần dùng chung ListSections + GetTree: xác minh owner qua
// repo trước (bài của người khác 404 như không tồn tại), đọc PageIndex từ
// Qdrant với filter owner từ token, rồi merge summary DB khi bài đã compile
// done. Trả errors.Error để Render map đúng mã status.
func (s *articleService) loadSections(ctx context.Context, id int64) (*domain.Article, []domain.SectionSpan, map[string]string, error) {
	// Route đã qua auth middleware nên user_id chắc chắn có trong ctx.
	userID, _ := s.currentUser.UserID(ctx)
	article, err := s.articleRepo.GetByID(ctx, id, userID)
	if err != nil {
		if stderrors.Is(err, domain.ErrNotFound) {
			return nil, nil, nil, errors.NewCustomHttpError(http.StatusNotFound, http.StatusNotFound, "bài viết không tồn tại")
		}
		return nil, nil, nil, err
	}
	sections, err := s.vectors.ListSections(ctx, domain.SearchFilter{
		UserID:    userID,
		ArticleID: article.ID,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	// Chưa compile (compile_status != done) hoặc bài không có dòng nào thì
	// map rỗng — mục vẫn trả đủ tọa độ, client phân biệt qua compile_status.
	summaries := make(map[string]string)
	if article.CompileStatus == domain.QueueDone {
		rows, serr := s.sectionRepo.ListByArticle(ctx, article.ID)
		if serr != nil {
			return nil, nil, nil, serr
		}
		for _, row := range rows {
			summaries[row.Name] = row.Summary
		}
	}
	return article, sections, summaries, nil
}
