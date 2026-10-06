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
	// PasswordChangedAt là mốc đổi mật khẩu gần nhất — mọi JWT ký trước
	// mốc này đều bị middleware từ chối (đổi pass là đá hết token cũ).
	// Zero value với user cũ (trước khi có cột) nghĩa là "chưa từng đổi".
	PasswordChangedAt time.Time `json:"-"`
}

func (User) TableName() string {
	return "users"
}

type IUserRepositoryImpl interface {
	CreateUser(ctx context.Context, user *User) error
	UpdateUser(ctx context.Context, user *User) (*User, error)
	GetUserById(ctx context.Context, id int64) (*User, error)
	GetUserByUsername(ctx context.Context, username string) (*User, error)
	TouchLastLogin(ctx context.Context, id int64, at time.Time) error
	UpdatePassword(ctx context.Context, id int64, hash string, changedAt time.Time) error
	DeleteUser(ctx context.Context, id int64) error
	GetAllUser(ctx context.Context) ([]*User, error)
}
