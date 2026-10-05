package domain

import "gorm.io/gorm"

type UnitOfWork interface {
	Begin() error
	Commit() error
	Rollback() error
	Transaction(func(*gorm.DB) error) error
}
