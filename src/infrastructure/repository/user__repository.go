package repository

import (
	"context"
	"knowledge_ingestion/src/domain"
	"knowledge_ingestion/src/infrastructure/postgres"
)

type UserRepository struct {
	db postgres.IDB
}

func NewUserRepository(db postgres.IDB) domain.IUserRepositoryImpl {
	return &UserRepository{db: db}
}

func (u *UserRepository) CreateUser(ctx context.Context, user *domain.User) error {
	err := u.db.GetDB().Create(user)
	if err != nil {
		return err.Error
	}
	return nil
}

func (u *UserRepository) UpdateUser(ctx context.Context, user *domain.User) (*domain.User, error) {
	//TODO implement me
	panic("implement me")
}

func (u *UserRepository) GetUserById(ctx context.Context, id int64) (*domain.User, error) {
	//TODO implement me
	panic("implement me")
}

func (u *UserRepository) DeleteUser(ctx context.Context, id int64) error {
	//TODO implement me
	panic("implement me")
}

func (u *UserRepository) GetAllUsers(ctx context.Context) ([]*domain.User, error) {
	//TODO implement me
	panic("implement me")
}
