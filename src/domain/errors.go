package domain

import "errors"

// ErrNotFound báo row không tồn tại — repo dịch từ gorm.ErrRecordNotFound
// sang sentinel này ở mọi read-single để service không import gorm (DIP):
// service chỉ switch domain.ErrNotFound (errors.Is xuyên wrap).
var ErrNotFound = errors.New("không tồn tại")
