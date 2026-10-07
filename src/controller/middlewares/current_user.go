package middlewares

import "context"

// ctxKey là kiểu key riêng cho context — key string trần ("user_id") dễ
// đụng với package khác, còn type riêng thì không.
type ctxKey string

// ctxUserIDKey là key nhét user_id đã xác thực vào request context trong
// Handler — service đọc ra qua ICurrentUser mà không cần biết gin.
const ctxUserIDKey ctxKey = "user_id"

// ICurrentUser đọc identity đã xác thực từ context — tương đương
// ICurrentUser/IHttpContextAccessor bên .NET: fx inject một lần,
// service nào cần user_id thì gọi, khỏi truyền param lòng vòng.
type ICurrentUser interface {
	// UserID trả user_id mà auth Handler đã nhét vào ctx — false khi
	// route quên gắn middleware auth hoặc ctx không phải từ request.
	UserID(ctx context.Context) (int64, bool)
}

type currentUser struct{}

func NewCurrentUser() ICurrentUser {
	return &currentUser{}
}

func (u *currentUser) UserID(ctx context.Context) (int64, bool) {
	if ctx == nil {
		return 0, false
	}
	uid, ok := ctx.Value(ctxUserIDKey).(int64)
	return uid, ok
}

// withUserID nhét user_id đã verify vào request context — gọi trong
// Handler sau khi đối chiếu token với DB xong.
func withUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, ctxUserIDKey, userID)
}
