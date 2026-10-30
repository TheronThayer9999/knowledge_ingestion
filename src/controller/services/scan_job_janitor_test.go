package services

import (
	"context"
	"slices"
	"testing"
	"time"

	"knowledge_ingestion/src/domain"
)

// janitorRepoStub chỉ cài đúng 3 method janitor dùng — method khác PANIC qua
// interface nhúng, để test đỏ ngay nếu janitor bắt đầu đọc thêm dữ liệu.
type janitorRepoStub struct {
	domain.IOCRJobRepository

	expired  []*domain.OCRJob
	pages    map[int64][]*domain.OCRJobPage
	deleted  []int64
	askedNow time.Time
}

func (f *janitorRepoStub) ListExpired(_ context.Context, now time.Time, _ int) ([]*domain.OCRJob, error) {
	f.askedNow = now
	return f.expired, nil
}

func (f *janitorRepoStub) ListPages(_ context.Context, jobID int64, from, _ int) ([]*domain.OCRJobPage, error) {
	if from > 1 {
		return nil, nil
	}
	return f.pages[jobID], nil
}

func (f *janitorRepoStub) DeleteIDs(_ context.Context, ids []int64) (int64, error) {
	f.deleted = append(f.deleted, ids...)
	return int64(len(ids)), nil
}

// TestScanJanitorPurgesExpiredJobsAndObjects khoá hai hành vi dễ hỏng: mọi object
// của job hết hạn đều bị xoá (kể cả ảnh render từng trang), và key TRÙNG chỉ xoá
// một lần — job nguồn PDF có OutputKey == SourceKey nên xoá hai lần là vô ích.
func TestScanJanitorPurgesExpiredJobsAndObjects(t *testing.T) {
	repo := &janitorRepoStub{
		expired: []*domain.OCRJob{
			{SourceKey: "scan/a/source.pdf", OutputKey: "scan/a/source.pdf"},
			{SourceKey: "scan/b/source.png", OutputKey: "scan/b/output.pdf"},
		},
		pages: map[int64][]*domain.OCRJobPage{
			1: {{RenderKey: "scan/a/p1.jpg"}},
			2: {{RenderKey: "scan/b/p1.jpg"}},
		},
	}
	repo.expired[0].ID = 1
	repo.expired[1].ID = 2
	store := newStoreStub()

	got, err := NewScanJobJanitor(repo, store).PurgeExpired(context.Background())
	if err != nil || got != 2 {
		t.Fatalf("PurgeExpired = %d, %v; muốn 2 job", got, err)
	}
	if !slices.Equal(repo.deleted, []int64{1, 2}) {
		t.Fatalf("DeleteIDs nhận %v; muốn [1 2]", repo.deleted)
	}
	want := []string{
		"scan/a/p1.jpg", "scan/a/source.pdf",
		"scan/b/output.pdf", "scan/b/p1.jpg", "scan/b/source.png",
	}
	slices.Sort(store.deletedKeys)
	if !slices.Equal(store.deletedKeys, want) {
		t.Fatalf("object bị xoá = %v; muốn %v", store.deletedKeys, want)
	}
	if repo.askedNow.IsZero() {
		t.Fatal("ListExpired phải nhận mốc thời gian hiện tại")
	}
}

// TestScanJanitorEmptyIsNoop: không có job hết hạn thì không được gọi DeleteIDs
// (một câu DELETE với danh sách rỗng vẫn là một lượt quét bảng vô ích).
func TestScanJanitorEmptyIsNoop(t *testing.T) {
	repo := &janitorRepoStub{}
	store := newStoreStub()
	got, err := NewScanJobJanitor(repo, store).PurgeExpired(context.Background())
	if err != nil || got != 0 {
		t.Fatalf("PurgeExpired = %d, %v; muốn 0", got, err)
	}
	if len(repo.deleted) != 0 || len(store.deletedKeys) != 0 {
		t.Fatalf("không có job hết hạn mà vẫn xoá: %v %v", repo.deleted, store.deletedKeys)
	}
}
