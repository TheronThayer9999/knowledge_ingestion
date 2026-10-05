package repository

import (
	"context"
	"knowledge_ingestion/src/domain"

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
	// TODO implement me
	panic("implement me")
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
