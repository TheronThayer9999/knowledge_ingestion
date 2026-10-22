package services

import (
	"context"
	"time"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/domain"
)

// IRefreshTokenJanitor dọn refresh token đã chết — tách khỏi purge article
// vì đây là vòng đời auth, không phải dữ liệu tri thức.
type IRefreshTokenJanitor interface {
	PurgeExpired(ctx context.Context) (int64, error)
}

type refreshTokenJanitor struct {
	repo domain.IRefreshTokenRepository
}

func NewRefreshTokenJanitor(repo domain.IRefreshTokenRepository) IRefreshTokenJanitor {
	return &refreshTokenJanitor{repo: repo}
}

// PurgeExpired xoá token hết hạn hoặc đã thu hồi quá thời gian lưu giữ. Giữ
// lại 7 ngày để còn tra khi có sự cố (reuse detection, logout bất thường),
// sau đó xoá cứng — không có nghiệp vụ nào cần khôi phục token chết.
func (j *refreshTokenJanitor) PurgeExpired(ctx context.Context) (int64, error) {
	cutoff := time.Now().Add(-constants.REFRESH_TOKEN_RETENTION)
	deleted, err := j.repo.DeleteExpired(ctx, cutoff)
	if err != nil {
		return 0, err
	}
	if deleted > 0 {
		logs.Infow("auth: dọn refresh token hết hạn",
			"trace_id", utils.TraceIDFromCtx(ctx), "deleted", deleted, "before", cutoff)
	}
	return deleted, nil
}
