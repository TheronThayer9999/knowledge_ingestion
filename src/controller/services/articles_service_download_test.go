package services

import (
	"context"
	stderrors "errors"
	cerr "knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/storage"
	"knowledge_ingestion/src/domain"
	"net/http"
	"testing"
	"time"
)

// downloadStubStorage chỉ PresignedGetURL có việc — nhúng interface cho đủ hợp
// đồng IStorage (method khác gọi nhầm là panic) và ghi lại đúng 1 lần ký để test
// khoá tham số gửi xuống kho: key nào, tên file nào, hạn bao lâu.
type downloadStubStorage struct {
	storage.IStorage
	url, key, downloadAs string
	expiry               time.Duration
	err                  error
	called               bool
}

func (s *downloadStubStorage) PresignedGetURL(_ context.Context, key, downloadAs string, expiry time.Duration) (string, error) {
	s.called = true
	s.key, s.downloadAs, s.expiry = key, downloadAs, expiry
	if s.err != nil {
		return "", s.err
	}
	return s.url, nil
}

func newDownloadSvc(repo *sectionsStubArticles, st *downloadStubStorage) IArticleService {
	// categoryRepo/vectors/sectionRepo/heartbeatRepo nil: đường tải file chỉ
	// chạm repo bài + kho, gọi nhầm cái khác là panic — test khoá đúng phạm vi.
	return NewArticleService(repo, nil, stubUser{id: 7}, st, nil, nil, nil)
}

func TestArticleDownloadURLSignsStoredFile(t *testing.T) {
	repo := &sectionsStubArticles{article: &domain.Article{
		BaseModel:   domain.BaseModel{ID: 8},
		StorageKey:  "uploads/01a1.pdf",
		Name:        "Báo cáo nội bộ.pdf",
		ContentType: "application/pdf",
	}}
	st := &downloadStubStorage{url: "http://seaweedfs/app-uploads/uploads/01a1.pdf?X-Amz-Signature=abc"}
	res := newDownloadSvc(repo, st).DownloadURL(context.Background(), 8)
	if res.Err != nil {
		t.Fatalf("DownloadURL: %v", res.Err)
	}
	if res.Data.URL != st.url || res.Data.ContentType != "application/pdf" {
		t.Errorf("response sai: %+v", res.Data)
	}
	if res.Data.Filename != "Báo cáo nội bộ.pdf" {
		t.Errorf("filename = %q, muốn giữ nguyên tên user đặt", res.Data.Filename)
	}
	if res.Data.ExpiresAt.Before(time.Now().Add(4 * time.Minute)) {
		t.Errorf("expires_at phải còn ≥ 4 phút, nhận %v", res.Data.ExpiresAt)
	}
	// Kho phải nhận đúng key của bài + tên tải về + hạn 5 phút.
	if st.key != "uploads/01a1.pdf" || st.downloadAs != "Báo cáo nội bộ.pdf" || st.expiry != 5*time.Minute {
		t.Errorf("tham số ký sai: key=%q downloadAs=%q expiry=%v", st.key, st.downloadAs, st.expiry)
	}
	if repo.gotUser != 7 {
		t.Errorf("repo phải được gọi với owner từ token, nhận %d", repo.gotUser)
	}
}

// Bài tạo trước đợt "giữ tên file" bị FE cắt đuôi — tải về phải có đuôi để mở
// đúng app, đuôi lấy từ storage key (do BE sinh, luôn chuẩn).
func TestArticleDownloadURLRestoresMissingExtension(t *testing.T) {
	repo := &sectionsStubArticles{article: &domain.Article{
		BaseModel:  domain.BaseModel{ID: 9},
		StorageKey: "uploads/01a2.docx",
		Name:       "Quy chế làm việc",
	}}
	st := &downloadStubStorage{url: "http://x/y"}
	res := newDownloadSvc(repo, st).DownloadURL(context.Background(), 9)
	if res.Err != nil {
		t.Fatalf("DownloadURL: %v", res.Err)
	}
	if res.Data.Filename != "Quy chế làm việc.docx" {
		t.Errorf("filename = %q, muốn %q", res.Data.Filename, "Quy chế làm việc.docx")
	}
}

// Bài nguồn URL không có blob trong kho: 400 kèm lý do, TUYỆT ĐỐI không ký.
func TestArticleDownloadURLUrlSourceIs400(t *testing.T) {
	repo := &sectionsStubArticles{article: &domain.Article{
		BaseModel: domain.BaseModel{ID: 10},
		URL:       "https://example.com/docs/go-fx",
		Name:      "Giới thiệu Go Fx",
	}}
	st := &downloadStubStorage{url: "http://x/y"}
	res := newDownloadSvc(repo, st).DownloadURL(context.Background(), 10)
	if res.Err == nil {
		t.Fatal("muốn 400, nhận ok")
	}
	e := cerr.From(res.Err)
	if e == nil || e.GetHttpCode() != http.StatusBadRequest {
		t.Fatalf("không phải 400: %v", res.Err)
	}
	if st.called {
		t.Error("bài không có file mà vẫn ký URL xuống kho")
	}
}

// Bài của người khác (repo scope owner trả ErrNotFound) phải 404 như không tồn
// tại, và không được ký gì cả.
func TestArticleDownloadURLForeignArticleIs404(t *testing.T) {
	st := &downloadStubStorage{url: "http://x/y"}
	res := newDownloadSvc(&sectionsStubArticles{err: domain.ErrNotFound}, st).
		DownloadURL(context.Background(), 99)
	if res.Err == nil {
		t.Fatal("muốn 404, nhận ok")
	}
	if e := cerr.From(res.Err); e == nil || e.GetHttpCode() != http.StatusNotFound {
		t.Fatalf("không phải 404: %v", res.Err)
	}
	if st.called {
		t.Error("bài không thuộc owner mà vẫn ký URL")
	}
}

// Lỗi kho trả thẳng lên trên để Render map thành 500 — service không tự bịa
// thông điệp, cũng không ký nhầm response thành công.
func TestArticleDownloadURLStorageErrorPropagates(t *testing.T) {
	boom := stderrors.New("storage down")
	res := newDownloadSvc(
		&sectionsStubArticles{article: &domain.Article{
			BaseModel:  domain.BaseModel{ID: 11},
			StorageKey: "uploads/01a3.pdf",
			Name:       "a.pdf",
		}},
		&downloadStubStorage{err: boom},
	).DownloadURL(context.Background(), 11)
	if !stderrors.Is(res.Err, boom) {
		t.Fatalf("err = %v, muốn %v", res.Err, boom)
	}
}
