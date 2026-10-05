package domain

import (
	"context"
	"time"
)

type User struct {
	BaseModel
	UserName  string     `json:"user_name"`
	Password  string     `json:"-"`
	Email     string     `json:"email"`
	Phone     string     `json:"phone"`
	Avatar    string     `json:"avatar"`
	Active    bool       `json:"active"`
	Role      int        `json:"role"`
	LastLogin *time.Time `json:"last_login"`
}

func (User) TableName() string {
	return "users"
}

type IUserRepositoryImpl interface {
	CreateUser(ctx context.Context, user *User) error
	UpdateUser(ctx context.Context, user *User) (*User, error)
	GetUserById(ctx context.Context, id int64) (*User, error)
	DeleteUser(ctx context.Context, id int64) error
	GetAllUser(ctx context.Context) ([]*User, error)
}
