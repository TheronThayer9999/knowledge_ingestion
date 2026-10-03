package models

import "time"

type User struct {
	UserName  string     `json:"user_name"`
	Password  string     `json:"-"`
	Email     string     `json:"email"`
	Phone     string     `json:"phone"`
	Avatar    string     `json:"avatar"`
	Active    bool       `json:"active"`
	Role      string     `json:"role"`
	LastLogin *time.Time `json:"last_login"`
	BaseModel
}
