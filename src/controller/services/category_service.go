package services

import (
	"context"
	stderrors "errors"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
	"net/http"
)

type ICategoryService interface {
	Create(ctx context.Context, userId int64, dto *dtos.CreateCategoryRequest) dtos.Result[*dtos.CategoryNodeResponse]
	GetById(ctx context.Context, id int64, userId int64) dtos.Result[*dtos.CategoryNodeResponse]
	GetByName(ctx context.Context, name string, userId int64) dtos.Result[*dtos.CategoryNodeResponse]
	GetAll(ctx context.Context, userId int64) dtos.Result[dtos.CategoryListResponse]
	Update(ctx context.Context, id int64, userId int64, dto *dtos.UpdateCategoryRequest) dtos.Result[*dtos.CategoryNodeResponse]
	Delete(ctx context.Context, id int64, userId int64) dtos.Result[*dtos.CategoryResponse]
}

type categoryService struct {
	categoryRepo        domain.ICategoryRepositoryImpl
	unitOfWork          domain.IUnitOfWork
	categoryClosureRepo domain.ICategoryClosureRepositoryImpl
	articleCascade      domain.ArticleCategoryCascade
}

func NewCategoryService(
	categoryRepo domain.ICategoryRepositoryImpl,
	unitOfWork domain.IUnitOfWork,
	categoryClosureRepo domain.ICategoryClosureRepositoryImpl,
	articleCascade domain.ArticleCategoryCascade) ICategoryService {
	return &categoryService{
		categoryRepo:        categoryRepo,
		unitOfWork:          unitOfWork,
		categoryClosureRepo: categoryClosureRepo,
		articleCascade:      articleCascade,
	}
}

// Create tạo danh mục cho đúng owner — ParentID khác nil thì cha phải tồn
// tại VÀ cùng owner. ParentID nil nghĩa là tạo root (user được nhiều root).
func (c *categoryService) Create(ctx context.Context, userId int64, dto *dtos.CreateCategoryRequest) dtos.Result[*dtos.CategoryNodeResponse] {
	if dto.ParentID != nil {
		ok, err := c.categoryRepo.Exists(ctx, *dto.ParentID, userId)
		if err != nil {
			return dtos.Fail[*dtos.CategoryNodeResponse](err)
		}
		if !ok {
			return dtos.Fail[*dtos.CategoryNodeResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "danh mục cha không tồn tại"))
		}
	}
	category := &domain.Category{
		UserID:      userId,
		Name:        dto.Name,
		Description: dto.Description,
	}
	err := c.unitOfWork.InTx(ctx, func(txCtx context.Context) error {
		// Insert category TRƯỚC để gorm nhận ID từ RETURNING — closure cần
		// ID thật, không được ghi trước rồi tự đặt ID như bản cũ.
		if err := c.categoryRepo.Create(txCtx, category); err != nil {
			return err
		}
		return c.categoryClosureRepo.AddToTree(txCtx, category.ID, dto.ParentID)
	})

	if err != nil {
		return dtos.Fail[*dtos.CategoryNodeResponse](err)
	}
	parentID, depth, err := c.placement(ctx, category.ID)
	if err != nil {
		return dtos.Fail[*dtos.CategoryNodeResponse](err)
	}
	return dtos.Ok(dtos.ToCategoryNodeResponse(category, parentID, depth))
}

// GetAll trả danh mục của đúng owner kèm vị trí cây (parent_id + depth).
// Cần thêm 1 query closure để client dựng cây, không N+1.
func (c *categoryService) GetAll(ctx context.Context, userId int64) dtos.Result[dtos.CategoryListResponse] {
	categories, err := c.categoryRepo.GetAll(ctx, userId)
	if err != nil {
		return dtos.Fail[dtos.CategoryListResponse](err)
	}
	rows, err := c.categoryClosureRepo.GetByOwner(ctx, userId)
	if err != nil {
		return dtos.Fail[dtos.CategoryListResponse](err)
	}
	parentOf := make(map[int64]int64, len(rows))
	levelOf := make(map[int64]int, len(rows))
	for _, r := range rows {
		if r.Depth == domain.DirectChildDepth {
			parentOf[r.DescendantID] = r.AncestorID // depth = 1 chính là cha
		}
		if r.Depth > levelOf[r.DescendantID] {
			levelOf[r.DescendantID] = r.Depth // max depth = cấp tuyệt đối
		}
	}
	return dtos.Ok(dtos.ToCategoryListResponse(categories, parentOf, levelOf))
}

// GetById trả 404 khi id không tồn tại HOẶC không thuộc owner.
func (c *categoryService) GetById(ctx context.Context, id int64, userId int64) dtos.Result[*dtos.CategoryNodeResponse] {
	category, err := c.categoryRepo.GetById(ctx, id, userId)
	if err != nil {
		if stderrors.Is(err, domain.ErrNotFound) {
			return dtos.Fail[*dtos.CategoryNodeResponse](errors.NewCustomHttpError(http.StatusNotFound, http.StatusNotFound, "danh mục không tồn tại"))
		}
		return dtos.Fail[*dtos.CategoryNodeResponse](err)
	}
	parentID, depth, err := c.placement(ctx, category.ID)
	if err != nil {
		return dtos.Fail[*dtos.CategoryNodeResponse](err)
	}
	return dtos.Ok(dtos.ToCategoryNodeResponse(category, parentID, depth))
}

// GetByName tìm theo tên chính xác trong phạm vi owner.
func (c *categoryService) GetByName(ctx context.Context, name string, userId int64) dtos.Result[*dtos.CategoryNodeResponse] {
	category, err := c.categoryRepo.GetByName(ctx, name, userId)
	if err != nil {
		if stderrors.Is(err, domain.ErrNotFound) {
			return dtos.Fail[*dtos.CategoryNodeResponse](errors.NewCustomHttpError(http.StatusNotFound, http.StatusNotFound, "danh mục không tồn tại"))
		}
		return dtos.Fail[*dtos.CategoryNodeResponse](err)
	}
	parentID, depth, err := c.placement(ctx, category.ID)
	if err != nil {
		return dtos.Fail[*dtos.CategoryNodeResponse](err)
	}
	return dtos.Ok(dtos.ToCategoryNodeResponse(category, parentID, depth))
}

// Update sửa từng phần — field nil giữ nguyên. Đổi cha thì cha mới phải
// cùng owner, không phải chính nó và không nằm trong subtree của nó; sau khi
// sửa row thì Move closure trong cùng transaction.
func (c *categoryService) Update(ctx context.Context, id int64, userId int64, dto *dtos.UpdateCategoryRequest) dtos.Result[*dtos.CategoryNodeResponse] {
	category, err := c.categoryRepo.GetById(ctx, id, userId)
	if err != nil {
		if stderrors.Is(err, domain.ErrNotFound) {
			return dtos.Fail[*dtos.CategoryNodeResponse](errors.NewCustomHttpError(http.StatusNotFound, http.StatusNotFound, "danh mục không tồn tại"))
		}
		return dtos.Fail[*dtos.CategoryNodeResponse](err)
	}
	if dto.MoveToRoot != nil && *dto.MoveToRoot && dto.ParentID != nil {
		return dtos.Fail[*dtos.CategoryNodeResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "chỉ gửi một trong parent_id hoặc move_to_root"))
	}
	if dto.ParentID != nil {
		if *dto.ParentID == category.ID {
			return dtos.Fail[*dtos.CategoryNodeResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "danh mục không thể là cha của chính nó"))
		}
		if _, err := c.categoryRepo.GetById(ctx, *dto.ParentID, userId); err != nil {
			if stderrors.Is(err, domain.ErrNotFound) {
				return dtos.Fail[*dtos.CategoryNodeResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "danh mục cha không tồn tại"))
			}
			return dtos.Fail[*dtos.CategoryNodeResponse](err)
		}
		// Cha mới nằm trong subtree của chính nó → cycle.
		isDescendant, err := c.categoryClosureRepo.IsAncestor(ctx, category.ID, *dto.ParentID)
		if err != nil {
			return dtos.Fail[*dtos.CategoryNodeResponse](err)
		}
		if isDescendant {
			return dtos.Fail[*dtos.CategoryNodeResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "không thể chuyển danh mục vào nhánh con của chính nó"))
		}
	}
	if dto.Name != nil {
		category.Name = *dto.Name
	}
	if dto.Description != nil {
		category.Description = *dto.Description
	}
	err = c.unitOfWork.InTx(ctx, func(txCtx context.Context) error {
		if err := c.categoryRepo.Update(txCtx, category); err != nil {
			return err
		}
		switch {
		case dto.ParentID != nil:
			return c.categoryClosureRepo.Move(txCtx, category.ID, dto.ParentID)
		case dto.MoveToRoot != nil && *dto.MoveToRoot:
			return c.categoryClosureRepo.Move(txCtx, category.ID, nil)
		}
		return nil
	})
	if err != nil {
		return dtos.Fail[*dtos.CategoryNodeResponse](err)
	}
	parentID, depth, err := c.placement(ctx, category.ID)
	if err != nil {
		return dtos.Fail[*dtos.CategoryNodeResponse](err)
	}
	return dtos.Ok(dtos.ToCategoryNodeResponse(category, parentID, depth))
}

// Delete xoá cả cây: toàn bộ folder con bên dưới và mọi bài viết trong các
// folder đó bị xoá mềm trong CÙNG transaction — không thể xoá folder cha
// xong chết giữa chừng để lại bài mồ côi. Bài đã xoá mềm được purge worker
// dọn blob + vector sau grace như xoá lẻ.
func (c *categoryService) Delete(ctx context.Context, id int64, userId int64) dtos.Result[*dtos.CategoryResponse] {
	category, err := c.categoryRepo.GetById(ctx, id, userId)
	if err != nil {
		if stderrors.Is(err, domain.ErrNotFound) {
			return dtos.Fail[*dtos.CategoryResponse](errors.NewCustomHttpError(http.StatusNotFound, http.StatusNotFound, "danh mục không tồn tại"))
		}
		return dtos.Fail[*dtos.CategoryResponse](err)
	}
	var folderIDs []int64
	var deletedArticles int64
	err = c.unitOfWork.InTx(ctx, func(txCtx context.Context) error {
		subtree, err := c.categoryClosureRepo.GetDescendants(txCtx, category.ID)
		if err != nil {
			return err
		}
		// Self row luôn có trong descendants; vẫn khởi tạo bằng chính nó để
		// danh sách không rỗng kể cả khi dữ liệu cũ thiếu self row.
		folderIDs = make([]int64, 0, len(subtree)+1)
		folderIDs = append(folderIDs, category.ID)
		for _, node := range subtree {
			if node.ID != category.ID {
				folderIDs = append(folderIDs, node.ID)
			}
		}
		if deletedArticles, err = c.articleCascade.DeleteByCategoryIDs(txCtx, folderIDs); err != nil {
			return err
		}
		if err := c.categoryClosureRepo.RemoveSubtree(txCtx, folderIDs); err != nil {
			return err
		}
		return c.categoryRepo.DeleteByIDs(txCtx, folderIDs)
	})
	if err != nil {
		return dtos.Fail[*dtos.CategoryResponse](err)
	}
	logs.Infow("category deleted", "trace_id", utils.TraceIDFromCtx(ctx), "category_id", category.ID,
		"folders", len(folderIDs), "articles", deletedArticles)
	return dtos.Ok(dtos.ToCategoryResponse(category))
}

// placement trả vị trí của category trong cây từ closure: cha trực tiếp
// (dòng depth = DirectChildDepth) và cấp tuyệt đối (max depth). Root trả
// (nil, 0). Gọi SAU khi đã ghi để đọc được trạng thái mới nhất.
func (c *categoryService) placement(ctx context.Context, categoryID int64) (*int64, int, error) {
	ancestors, err := c.categoryClosureRepo.GetAncestors(ctx, categoryID)
	if err != nil {
		return nil, 0, err
	}
	var parentID *int64
	depth := 0
	for _, a := range ancestors {
		if a.Depth == domain.DirectChildDepth {
			pid := a.ID
			parentID = &pid
		}
		if a.Depth > depth {
			depth = a.Depth
		}
	}
	return parentID, depth, nil
}
