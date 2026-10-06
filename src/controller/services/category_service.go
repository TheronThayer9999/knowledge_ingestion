package services

import (
	"context"
	stderrors "errors"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
	"net/http"

	"gorm.io/gorm"
)

type ICategoryService interface {
	Create(ctx context.Context, userId int64, dto *dtos.CreateCategoryRequest) dtos.Result[*dtos.CategoryResponse]
	GetById(ctx context.Context, id int64, userId int64) dtos.Result[*dtos.CategoryResponse]
	GetByName(ctx context.Context, name string, userId int64) dtos.Result[*dtos.CategoryResponse]
	GetAll(ctx context.Context, userId int64) dtos.Result[dtos.CategoryListResponse]
	Update(ctx context.Context, id int64, userId int64, dto *dtos.UpdateCategoryRequest) dtos.Result[*dtos.CategoryResponse]
	Delete(ctx context.Context, id int64, userId int64) dtos.Result[*dtos.CategoryResponse]
}

type categoryService struct {
	categoryRepo domain.ICategoryRepositoryImpl
}

func NewCategoryService(categoryRepo domain.ICategoryRepositoryImpl) ICategoryService {
	return &categoryService{
		categoryRepo: categoryRepo,
	}
}

// Create tạo danh mục cho đúng owner — ParentID khác nil thì cha phải tồn
// tại VÀ cùng owner. ParentID nil nghĩa là tạo root (mỗi user 1 root).
func (c *categoryService) Create(ctx context.Context, userId int64, dto *dtos.CreateCategoryRequest) dtos.Result[*dtos.CategoryResponse] {
	if dto.ParentID != nil {
		if _, err := c.categoryRepo.GetById(ctx, *dto.ParentID, userId); err != nil {
			if stderrors.Is(err, gorm.ErrRecordNotFound) {
				return dtos.Fail[*dtos.CategoryResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "danh mục cha không tồn tại"))
			}
			return dtos.Fail[*dtos.CategoryResponse](err)
		}
	}
	category := &domain.Category{
		UserID:      userId,
		Name:        dto.Name,
		Description: dto.Description,
		ParentID:    dto.ParentID,
	}
	if err := c.categoryRepo.Create(ctx, category); err != nil {
		return dtos.Fail[*dtos.CategoryResponse](err)
	}
	return dtos.Ok(dtos.ToCategoryResponse(category))
}

// GetAll chỉ trả danh mục của đúng owner.
func (c *categoryService) GetAll(ctx context.Context, userId int64) dtos.Result[dtos.CategoryListResponse] {
	categories, err := c.categoryRepo.GetAll(ctx, userId)
	if err != nil {
		return dtos.Fail[dtos.CategoryListResponse](err)
	}
	return dtos.Ok(dtos.ToCategoryListResponse(categories))
}

// GetById trả 404 khi id không tồn tại HOẶC không thuộc owner.
func (c *categoryService) GetById(ctx context.Context, id int64, userId int64) dtos.Result[*dtos.CategoryResponse] {
	category, err := c.categoryRepo.GetById(ctx, id, userId)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return dtos.Fail[*dtos.CategoryResponse](errors.NewCustomHttpError(http.StatusNotFound, http.StatusNotFound, "danh mục không tồn tại"))
		}
		return dtos.Fail[*dtos.CategoryResponse](err)
	}
	return dtos.Ok(dtos.ToCategoryResponse(category))
}

// GetByName tìm theo tên chính xác trong phạm vi owner.
func (c *categoryService) GetByName(ctx context.Context, name string, userId int64) dtos.Result[*dtos.CategoryResponse] {
	category, err := c.categoryRepo.GetByName(ctx, name, userId)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return dtos.Fail[*dtos.CategoryResponse](errors.NewCustomHttpError(http.StatusNotFound, http.StatusNotFound, "danh mục không tồn tại"))
		}
		return dtos.Fail[*dtos.CategoryResponse](err)
	}
	return dtos.Ok(dtos.ToCategoryResponse(category))
}

// Update sửa từng phần — field nil giữ nguyên. Row load đã scope owner nên
// không thể sửa hộ. Đổi cha thì cha mới phải cùng owner và không tự nhận
// mình làm cha.
func (c *categoryService) Update(ctx context.Context, id int64, userId int64, dto *dtos.UpdateCategoryRequest) dtos.Result[*dtos.CategoryResponse] {
	category, err := c.categoryRepo.GetById(ctx, id, userId)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return dtos.Fail[*dtos.CategoryResponse](errors.NewCustomHttpError(http.StatusNotFound, http.StatusNotFound, "danh mục không tồn tại"))
		}
		return dtos.Fail[*dtos.CategoryResponse](err)
	}
	if dto.Name != nil {
		category.Name = *dto.Name
	}
	if dto.Description != nil {
		category.Description = *dto.Description
	}
	if dto.ParentID != nil {
		if *dto.ParentID == category.ID {
			return dtos.Fail[*dtos.CategoryResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "danh mục không thể là cha của chính nó"))
		}
		if _, err := c.categoryRepo.GetById(ctx, *dto.ParentID, userId); err != nil {
			if stderrors.Is(err, gorm.ErrRecordNotFound) {
				return dtos.Fail[*dtos.CategoryResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "danh mục cha không tồn tại"))
			}
			return dtos.Fail[*dtos.CategoryResponse](err)
		}
		category.ParentID = dto.ParentID
	}
	if err := c.categoryRepo.Update(ctx, category); err != nil {
		return dtos.Fail[*dtos.CategoryResponse](err)
	}
	return dtos.Ok(dtos.ToCategoryResponse(category))
}

// Delete xóa danh mục của mình — còn danh mục con thì FK RESTRICT ở DB chặn
// lại, map về 400 để client hiểu.
func (c *categoryService) Delete(ctx context.Context, id int64, userId int64) dtos.Result[*dtos.CategoryResponse] {
	category, err := c.categoryRepo.GetById(ctx, id, userId)
	if err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return dtos.Fail[*dtos.CategoryResponse](errors.NewCustomHttpError(http.StatusNotFound, http.StatusNotFound, "danh mục không tồn tại"))
		}
		return dtos.Fail[*dtos.CategoryResponse](err)
	}
	if err := c.categoryRepo.Delete(ctx, category); err != nil {
		return dtos.Fail[*dtos.CategoryResponse](errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "không thể xóa danh mục còn danh mục con"))
	}
	return dtos.Ok(dtos.ToCategoryResponse(category))
}
