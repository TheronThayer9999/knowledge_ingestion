package repository

import (
	"context"
	"errors"
	"knowledge_ingestion/src/domain"
	"time"

	"gorm.io/gorm"
)

type UserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) domain.IUserRepositoryImpl {
	return &UserRepository{db: db}
}

func (u *UserRepository) CreateUser(ctx context.Context, user *domain.User) error {
	err := u.db.WithContext(ctx).Create(user)
	if err != nil {
		return err.Error
	}
	return nil
}

func (u *UserRepository) UpdateUser(ctx context.Context, user *domain.User) (*domain.User, error) {
	// TODO implement me
	panic("implement me")
}

func (u *UserRepository) GetUserById(ctx context.Context, id int64) (*domain.User, error) {
	var user domain.User
	if err := u.db.WithContext(ctx).First(&user, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (u *UserRepository) GetUserByUsername(ctx context.Context, username string) (*domain.User, error) {
	var user domain.User
	if err := u.db.WithContext(ctx).Where("user_name = ?", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err // service tự map 401
	}
	return &user, nil
}

// TouchLastLogin cập nhật mốc login gần nhất — login vẫn thành công dù bước này lỗi.
func (u *UserRepository) TouchLastLogin(ctx context.Context, id int64, at time.Time) error {
	return u.db.WithContext(ctx).Model(&domain.User{}).Where("id = ?", id).Update("last_login", at).Error
}

// UpdatePassword đổi hash mật khẩu đồng thời chốt mốc PasswordChangedAt —
// mốc này làm mọi JWT ký trước đó hết hiệu lực ngay lập tức.
func (u *UserRepository) UpdatePassword(ctx context.Context, id int64, hash string, changedAt time.Time) error {
	return u.db.WithContext(ctx).Model(&domain.User{}).Where("id = ?", id).Updates(map[string]interface{}{
		"password":            hash,
		"password_changed_at": changedAt,
	}).Error
}

func (u *UserRepository) DeleteUser(ctx context.Context, id int64) error {
	// TODO implement me
	panic("implement me")
}

func (u *UserRepository) GetAllUser(ctx context.Context) ([]*domain.User, error) {
	var users []*domain.User
	err := u.db.WithContext(ctx).Find(&users).Error
	if err != nil {
		return nil, err
	}
	return users, nil
}
