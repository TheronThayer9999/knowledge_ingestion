package services

import (
	"context"
	"time"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/storage"
	"knowledge_ingestion/src/domain"
)

// IScanJobJanitor dọn bản quét quá hạn (scan_ocr.temp_ttl_hours). Không có
// janitor thì mọi job người dùng bỏ dở nằm lại bucket vĩnh viễn: tab Quét là
// luồng TẠM, không phải kho tri thức, nên TTL phải được THI HÀNH chứ không chỉ
// ghi vào cột expires_at.
type IScanJobJanitor interface {
	PurgeExpired(ctx context.Context) (int, error)
}

type scanJobJanitor struct {
	jobs  domain.IOCRJobRepository
	store storage.IStorage
}

func NewScanJobJanitor(jobs domain.IOCRJobRepository, store storage.IStorage) IScanJobJanitor {
	return &scanJobJanitor{jobs: jobs, store: store}
}

// PurgeExpired xoá object TRƯỚC rồi mới xoá row: ngược lại thì row mất là mất
// luôn đầu mối duy nhất dẫn tới key, object thành rác không ai biết. Chạy theo
// lô (ListExpired) nên bảng lớn cũng không kéo hết lên RAM một lượt.
func (j *scanJobJanitor) PurgeExpired(ctx context.Context) (int, error) {
	expired, err := j.jobs.ListExpired(ctx, time.Now(), constants.SCAN_JANITOR_BATCH)
	if err != nil {
		return 0, err
	}
	if len(expired) == 0 {
		return 0, nil
	}
	ids := make([]int64, 0, len(expired))
	objects := 0
	for _, job := range expired {
		keys := jobKeys(ctx, j.jobs, job.ID)
		objects += removeObjects(ctx, j.store, append(keys, job.SourceKey, job.OutputKey))
		ids = append(ids, job.ID)
	}
	removed, err := j.jobs.DeleteIDs(ctx, ids)
	if err != nil {
		return int(removed), err
	}
	logs.Infow("scan-janitor: dọn bản quét hết hạn", "jobs", removed, "objects", objects)
	return int(removed), nil
}
