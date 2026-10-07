package domain

import "context"

// IUnitOfWork gom nhiều repo op vào đúng 1 transaction — mọi repo op gọi
// trong fn (qua ctx) đều chung tx, lỗi bất kỳ đâu là rollback toàn bộ.
// Tx sống và chết bên trong implementation (infrastructure), service chỉ
// thấy contract này: không gorm, không Begin/Commit/Rollback thủ công nên
// không có đường quên đóng transaction.
type IUnitOfWork interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}
