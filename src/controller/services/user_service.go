package services

import (
	"context"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"

	"golang.org/x/crypto/bcrypt"
)

type IUserService interface {
	Create(ctx context.Context, dto *dtos.CreateUserRequest) dtos.Result[*dtos.UserResponse]
	GetAllUser(ctx context.Context) dtos.Result[dtos.UserListResponse]
}

type userService struct {
	userRepo domain.IUserRepositoryImpl
}

func NewUserService(userRepo domain.IUserRepositoryImpl) IUserService {
	return &userService{
		userRepo: userRepo,
	}
}

func (u *userService) Create(ctx context.Context, dto *dtos.CreateUserRequest) dtos.Result[*dtos.UserResponse] {
	hash, err := bcrypt.GenerateFromPassword([]byte(dto.Password), bcrypt.DefaultCost)
	if err != nil {
		return dtos.Fail[*dtos.UserResponse](err)
	}
	user := dto.ToModel()
	user.Password = string(hash)
	user.Active = true

	if err := u.userRepo.CreateUser(ctx, user); err != nil {
		return dtos.Fail[*dtos.UserResponse](err)
	}
	return dtos.Ok(dtos.ToUserResponse(user))
}

func (u *userService) GetAllUser(ctx context.Context) dtos.Result[dtos.UserListResponse] {
	users, err := u.userRepo.GetAllUser(ctx)
	if err != nil {
		return dtos.Fail[dtos.UserListResponse](err)
	}
	return dtos.Ok(dtos.ToUserListResponse(users))
}
