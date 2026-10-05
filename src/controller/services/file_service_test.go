package services

import (
	"context"
	"errors"
	"io"
	"knowledge_ingestion/src/common/storage"
	"knowledge_ingestion/src/controller/dtos"
	"net/http"
	"strings"
	"testing"
	"time"
)

type fakeStorage struct {
	lastKey    string
	lastExpiry time.Duration
}

func (f *fakeStorage) Upload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	return nil
}

func (f *fakeStorage) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	return nil, nil
}

func (f *fakeStorage) Delete(ctx context.Context, key string) error { return nil }

func (f *fakeStorage) PresignedURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	f.lastKey = key
	f.lastExpiry = expiry
	return "http://storage/" + key + "?X-Amz-Signature=abc", nil
}

var _ storage.IStorage = (*fakeStorage)(nil)

func TestBuildUploadKeyStripsPath(t *testing.T) {
	for _, name := range []string{"../../../etc/passwd", "..\\..\\secret.env", "/abs/path/file.pdf"} {
		key, err := buildUploadKey(name)
		if err != nil {
			t.Fatalf("buildUploadKey(%q) error: %v", name, err)
		}
		if !strings.HasPrefix(key, uploadPrefix) {
			t.Errorf("buildUploadKey(%q) = %q, want prefix %q", name, key, uploadPrefix)
		}
		if strings.Contains(key, "..") || strings.Count(key, "/") != strings.Count(uploadPrefix, "/") {
			t.Errorf("buildUploadKey(%q) = %q, key must not escape the prefix", name, key)
		}
	}
}

func TestBuildUploadKeyIsUnique(t *testing.T) {
	a, err := buildUploadKey("file.pdf")
	if err != nil {
		t.Fatalf("buildUploadKey error: %v", err)
	}
	b, err := buildUploadKey("file.pdf")
	if err != nil {
		t.Fatalf("buildUploadKey error: %v", err)
	}
	if a == b {
		t.Errorf("two keys for the same filename are identical: %q", a)
	}
}

func TestBuildUploadKeyKeepsExtension(t *testing.T) {
	key, err := buildUploadKey("Báo Cáo.PDF")
	if err != nil {
		t.Fatalf("buildUploadKey error: %v", err)
	}
	if !strings.HasSuffix(key, ".pdf") {
		t.Errorf("buildUploadKey = %q, want lowercase extension .pdf", key)
	}
}

func TestBuildUploadKeyRejectsEmpty(t *testing.T) {
	for _, name := range []string{"", "   ", "...", "/", "\\", ".   ."} {
		if _, err := buildUploadKey(name); err == nil {
			t.Errorf("buildUploadKey(%q) expected error, got nil", name)
		}
	}
}

func TestPresignUploadReturnsPUTURL(t *testing.T) {
	st := &fakeStorage{}
	svc := NewFileService(st)

	res := svc.PresignUpload(context.Background(), &dtos.PresignUploadRequest{
		Filename:    "bao-cao.pdf",
		ContentType: "application/pdf",
	})
	if res.Err != nil {
		t.Fatalf("PresignUpload error: %v", res.Err)
	}
	if res.Data.Method != http.MethodPut {
		t.Errorf("Method = %q, want PUT", res.Data.Method)
	}
	if !strings.HasPrefix(res.Data.Key, uploadPrefix) {
		t.Errorf("Key = %q, want prefix %q", res.Data.Key, uploadPrefix)
	}
	if !strings.Contains(res.Data.UploadURL, res.Data.Key) {
		t.Errorf("UploadURL %q must contain key %q", res.Data.UploadURL, res.Data.Key)
	}
	if st.lastKey != res.Data.Key {
		t.Errorf("storage called with key %q, response says %q", st.lastKey, res.Data.Key)
	}
	if st.lastExpiry != presignExpiry {
		t.Errorf("expiry = %v, want %v", st.lastExpiry, presignExpiry)
	}
	if until := time.Until(res.Data.ExpiresAt); until < presignExpiry-time.Minute || until > presignExpiry+time.Minute {
		t.Errorf("ExpiresAt = %v, want about %v from now", res.Data.ExpiresAt, presignExpiry)
	}
}

func TestPresignUploadRejectsBadFilename(t *testing.T) {
	svc := NewFileService(&fakeStorage{})

	res := svc.PresignUpload(context.Background(), &dtos.PresignUploadRequest{Filename: "..."})
	if res.Err == nil {
		t.Fatal("expected error for invalid filename")
	}
	var e interface{ GetHttpCode() int }
	if !errors.As(res.Err, &e) || e.GetHttpCode() != http.StatusBadRequest {
		t.Errorf("expected 400 client error, got %v", res.Err)
	}
}
