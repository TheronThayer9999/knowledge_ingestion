package dtos

// ## DTO Guidelines
//
// This section describes how to name, validate, and layer Data Transfer Objects (DTOs) in this project.
//
// ### Naming
//
// 1. **No `Dto` suffix.** The package is already named `dto`, so `dto.UserCreateDto` stutters. Use `dto.CreateUserRequest`.
// 2. **Requests are named `<Action><Entity>Request`**, e.g. `CreateUserRequest`, `UpdateUserRequest`.
// 3. **Responses are named `<Entity>Response`**, e.g. `UserResponse`, `UserListResponse`.
// 4. **Follow Go initialism rules** for fields: `Username` (not `UserName`), `ID` (not `Id`), `URL`, `HTTP`.
// 5. **JSON tags use a single style** (`snake_case` or `camelCase`) across the whole project. Do not mix.
//
// ### JSON Tags
//
// 6. **Request DTOs:** every field that must be received needs an explicit tag, e.g. `json:"password"`. Never use `json:"-"` on a request field: it makes `ShouldBindJSON` skip the field, so it is always empty.
// 7. **Response DTOs:** never declare sensitive fields (password, hash, token). Do not rely on `json:"-"` to hide them; the safe approach is to not have the field at all.
// 8. **Request and response are always separate structs.** Never share one struct for both.
//
// ### Validation
//
// 9. Put validation rules in the `binding` tag of request DTOs: `required`, `email`, `min`, `max`, `omitempty`.
// 10. For optional fields, combine `omitempty` with a rule (e.g. `omitempty,url`) so the rule only runs when a value is present.
// 11. For partial-update requests, use pointer fields (`*string`) to distinguish "not sent" from "sent empty".
//
// ### Layering
//
// 12. **DTOs live outside `domain`.** `domain` must never import `dto`. `dto` may import `models` (one direction only).
// 13. **Mappers** (`ToModel()`, `ToUserResponse()`) live in the same package as the DTOs and are written by hand. No mapping library is needed.
// 14. **Services call the mappers** (`ToModel()`, `ToUserResponse()`) to convert DTO <-> model; repositories work with models only. The controller just validates (`Bind`) and `Render`s — no mapping there.
// 15. **Never map a raw password into a model.** The service hashes it (bcrypt or argon2) and stores the result in `PasswordHash`.

import (
	"knowledge_ingestion/src/domain"
	"time"
)

type CreateUserRequest struct {
	Username string `json:"username" binding:"required,min=3,max=50" example:"johndoe"`
	Password string `json:"password" binding:"required,min=8" example:"secret123"`
	Email    string `json:"email" binding:"required,email" example:"john@example.com"`
	Phone    string `json:"phone" binding:"omitempty,e164" example:"+84901234567"`
	Avatar   string `json:"avatar" binding:"omitempty,url" example:"https://example.com/avatar.png"`
}

type UpdateUserRequest struct {
	Username *string `json:"username,omitempty" binding:"omitempty,min=3,max=50" example:"johndoe"`
	Email    *string `json:"email,omitempty" binding:"omitempty,email" example:"john@example.com"`
	Phone    *string `json:"phone,omitempty" binding:"omitempty,e164" example:"+84901234567"`
	Avatar   *string `json:"avatar,omitempty" binding:"omitempty,url" example:"https://example.com/avatar.png"`
}

type UserResponse struct {
	ID        int64      `json:"id" example:"1"`
	Username  string     `json:"username" example:"johndoe"`
	Email     string     `json:"email" example:"john@example.com"`
	Phone     string     `json:"phone" example:"+84901234567"`
	Avatar    string     `json:"avatar" example:"https://example.com/avatar.png"`
	Active    bool       `json:"active" example:"true"`
	Role      int        `json:"role" example:"0"`
	LastLogin *time.Time `json:"last_login,omitempty" example:"2026-10-03T10:00:00+07:00"`
}

type UserListResponse struct {
	Users []*UserResponse `json:"users"`
	Total int64           `json:"total" example:"10"`
}

// LoginUserRequest là payload đăng nhập — tìm user theo username rồi so
// hash bcrypt, đúng thì phát JWT.
type LoginUserRequest struct {
	Username string `json:"username" binding:"required" example:"johndoe"`
	Password string `json:"password" binding:"required" example:"secret123"`
}

// LoginUserResponse trả token + hạn + thông tin user (không bao giờ có password).
type LoginUserResponse struct {
	Token     string        `json:"token" example:"eyJhbGciOiJIUzI1NiIs..."`
	ExpiresAt time.Time     `json:"expires_at" example:"2026-10-07T10:00:00+07:00"`
	User      *UserResponse `json:"user"`
}

// ChangePasswordRequest đổi mật khẩu của chính mình — userId lấy từ token
// (middleware đã nhét vào context), không nhận id từ client để tránh đổi
// pass hộ người khác.
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required" example:"secret123"`
	NewPassword string `json:"new_password" binding:"required,min=8" example:"newsecret456"`
}

func ToUserResponse(user *domain.User) *UserResponse {
	if user == nil {
		return &UserResponse{}
	}
	return &UserResponse{
		ID:        user.ID,
		Username:  user.UserName,
		Email:     user.Email,
		Phone:     user.Phone,
		Avatar:    user.Avatar,
		Active:    user.Active,
		Role:      user.Role,
		LastLogin: user.LastLogin,
	}
}

// ToModel map request -> domain model, KHÔNG copy password (#15):
// service tự hash từ dto.Password trước khi lưu.
func (r *CreateUserRequest) ToModel() *domain.User {
	return &domain.User{
		UserName: r.Username,
		Email:    r.Email,
		Phone:    r.Phone,
		Avatar:   r.Avatar,
	}
}

func ToUserListResponse(users []*domain.User) UserListResponse {
	res := UserListResponse{
		Users: make([]*UserResponse, 0, len(users)),
		Total: int64(len(users)),
	}
	for _, u := range users {
		res.Users = append(res.Users, ToUserResponse(u))
	}
	return res
}
