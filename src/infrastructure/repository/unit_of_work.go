package repository

import (
	"context"
	"knowledge_ingestion/src/domain"

	"gorm.io/gorm"
)

// txKey là key nhét *gorm.DB vào ctx trong InTx — unexported nên chỉ
// package này đọc được, service không thể tự tạo hay giả mạo tx.
type txKey struct{}

// dbConn trả tx trong ctx nếu đang ở trong InTx, ngược lại trả db gốc.
// Mọi repo method gọi dbConn thay vì db.WithContext trực tiếp — có vậy các
// op trong cùng InTx mới chung 1 transaction thật.
func dbConn(ctx context.Context, db *gorm.DB) *gorm.DB {
	if tx, ok := ctx.Value(txKey{}).(*gorm.DB); ok && tx != nil {
		return tx.WithContext(ctx)
	}
	return db.WithContext(ctx)
}

type unitOfWork struct {
	db *gorm.DB
}

func NewUnitOfWork(db *gorm.DB) domain.IUnitOfWork {
	return &unitOfWork{db: db}
}

// InTx mở 1 transaction, nhét vào ctx rồi chạy fn — lỗi bất kỳ đâu là
// rollback toàn bộ, nil thì commit. Gorm tự đóng tx cả 2 đường nên caller
// không bao giờ leak.
func (u *unitOfWork) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return u.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(context.WithValue(ctx, txKey{}, tx))
	})
}
