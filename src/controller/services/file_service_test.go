package services

import (
	"bytes"
	"context"
	"errors"
	"io"
	"knowledge_ingestion/src/common/storage"
	"knowledge_ingestion/src/controller/dtos"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"
)

type fakeStorage struct {
	lastKey         string
	lastContentType string
	lastExpiry      time.Duration
	calls           int
}

func (f *fakeStorage) Upload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	return nil
}

func (f *fakeStorage) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	return nil, nil
}

func (f *fakeStorage) Delete(ctx context.Context, key string) error { return nil }

func (f *fakeStorage) PresignedURL(ctx context.Context, key string, contentType string, expiry time.Duration) (string, error) {
	f.lastKey = key
	f.lastContentType = contentType
	f.lastExpiry = expiry
	f.calls++
	return "http://storage/" + key + "?X-Amz-Signature=abc", nil
}

var _ storage.IStorage = (*fakeStorage)(nil)

// Nội dung mẫu có magic bytes thật để DetectContentType đoán đúng.
var (
	pdfContent = []byte("%PDF-1.4\n1 0 obj<</Type/Catalog>>\n")
	jpgContent = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
	pngContent = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00}
	txtContent = []byte("hello world plain text file\n")
	zipContent = []byte{'P', 'K', 0x03, 0x04, 0x14, 0x00, 0x00, 0x00}
	exeContent = []byte{'M', 'Z', 0x90, 0x00, 0x03, 0x00, 0x00, 0x00}
)

// newFileHeader dựng *multipart.FileHeader như gin nhận từ form —
// CreateFormFile luôn khai Content-Type part là application/octet-stream,
// đúng ý test: service phải lờ header này.
func newFileHeader(t *testing.T, filename string, content []byte) *multipart.FileHeader {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile error: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write part error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer error: %v", err)
	}
	form, err := multipart.NewReader(&buf, w.Boundary()).ReadForm(1 << 20)
	if err != nil {
		t.Fatalf("ReadForm error: %v", err)
	}
	files := form.File["file"]
	if len(files) == 0 {
		t.Fatal("no file parsed from multipart form")
	}
	return files[0]
}

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
		File: newFileHeader(t, "bao-cao.pdf", pdfContent),
	})
	if res.Err != nil {
		t.Fatalf("PresignUpload error: %v", res.Err)
	}
	if res.Data.Method != http.MethodPut {
		t.Errorf("Method = %q, want PUT", res.Data.Method)
	}
	if res.Data.ContentType != "application/pdf" {
		t.Errorf("ContentType = %q, want MIME do BE verify từ đuôi + nội dung", res.Data.ContentType)
	}
	if st.lastContentType != res.Data.ContentType {
		t.Errorf("chữ ký nhận contentType %q nhưng response báo %q", st.lastContentType, res.Data.ContentType)
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

	res := svc.PresignUpload(context.Background(), &dtos.PresignUploadRequest{
		File: newFileHeader(t, "...", pdfContent),
	})
	if res.Err == nil {
		t.Fatal("expected error for invalid filename")
	}
	var e interface{ GetHttpCode() int }
	if !errors.As(res.Err, &e) || e.GetHttpCode() != http.StatusBadRequest {
		t.Errorf("expected 400 client error, got %v", res.Err)
	}
}

func TestPresignUploadRejectsNilFile(t *testing.T) {
	svc := NewFileService(&fakeStorage{})

	res := svc.PresignUpload(context.Background(), &dtos.PresignUploadRequest{})
	if res.Err == nil {
		t.Fatal("expected error for nil file")
	}
	var e interface{ GetHttpCode() int }
	if !errors.As(res.Err, &e) || e.GetHttpCode() != http.StatusBadRequest {
		t.Errorf("expected 400 client error, got %v", res.Err)
	}
}

func TestPresignUploadRejectsContentMismatch(t *testing.T) {
	st := &fakeStorage{}
	svc := NewFileService(st)

	// .exe đổi tên thành .pdf — đuôi lọt whitelist nhưng magic bytes tố cáo.
	res := svc.PresignUpload(context.Background(), &dtos.PresignUploadRequest{
		File: newFileHeader(t, "bao-cao.pdf", exeContent),
	})
	if res.Err == nil {
		t.Fatal("expected error for exe content named .pdf")
	}
	var e interface{ GetHttpCode() int }
	if !errors.As(res.Err, &e) || e.GetHttpCode() != http.StatusBadRequest {
		t.Errorf("expected 400 client error, got %v", res.Err)
	}
	if st.calls != 0 {
		t.Errorf("không được ký URL khi nội dung lệch đuôi, storage called %d times", st.calls)
	}
}

func TestPresignUploadRejectsEmptyFile(t *testing.T) {
	svc := NewFileService(&fakeStorage{})

	res := svc.PresignUpload(context.Background(), &dtos.PresignUploadRequest{
		File: newFileHeader(t, "rong.pdf", nil),
	})
	if res.Err == nil {
		t.Fatal("expected error for empty file")
	}
}

func TestPresignUploadAcceptsOfficeZip(t *testing.T) {
	svc := NewFileService(&fakeStorage{})

	// .docx thật là file zip — DetectContentType chỉ thấy application/zip.
	res := svc.PresignUpload(context.Background(), &dtos.PresignUploadRequest{
		File: newFileHeader(t, "hop-dong.docx", zipContent),
	})
	if res.Err != nil {
		t.Fatalf("PresignUpload(.docx zip) error: %v", res.Err)
	}
	if res.Data.ContentType != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Errorf("ContentType = %q, want OOXML document MIME", res.Data.ContentType)
	}
}

func TestPresignUploadsKeepsOrderAndUniqueKeys(t *testing.T) {
	st := &fakeStorage{}
	svc := NewFileService(st)

	res := svc.PresignUploads(context.Background(), &dtos.PresignUploadsRequest{
		Files: []*dtos.PresignUploadFile{
			{File: newFileHeader(t, "a.pdf", pdfContent)},
			{File: newFileHeader(t, "b.jpg", jpgContent)},
			{File: newFileHeader(t, "c.png", pngContent)},
			{File: newFileHeader(t, "d.txt", txtContent)},
		},
	})
	if res.Err != nil {
		t.Fatalf("PresignUploads error: %v", res.Err)
	}
	if len(res.Data.Items) != 4 {
		t.Fatalf("got %d items, want 4", len(res.Data.Items))
	}

	want := []string{"a.pdf", "b.jpg", "c.png", "d.txt"}
	wantMime := []string{"application/pdf", "image/jpeg", "image/png", "text/plain"}
	seen := map[string]bool{}
	for i, item := range res.Data.Items {
		if item.Filename != want[i] {
			t.Errorf("item[%d].Filename = %q, want %q (thứ tự phải khớp request)", i, item.Filename, want[i])
		}
		if item.ContentType != wantMime[i] {
			t.Errorf("item[%d].ContentType = %q, want %q", i, item.ContentType, wantMime[i])
		}
		if item.Method != http.MethodPut {
			t.Errorf("item[%d].Method = %q, want PUT", i, item.Method)
		}
		if !strings.HasPrefix(item.Key, uploadPrefix) {
			t.Errorf("item[%d].Key = %q, want prefix %q", i, item.Key, uploadPrefix)
		}
		if !strings.Contains(item.UploadURL, item.Key) {
			t.Errorf("item[%d].UploadURL %q must contain key %q", i, item.UploadURL, item.Key)
		}
		if seen[item.Key] {
			t.Errorf("item[%d].Key %q bị trùng", i, item.Key)
		}
		seen[item.Key] = true
	}
	if st.calls != 4 {
		t.Errorf("storage.PresignedURL called %d times, want 4", st.calls)
	}
}

func TestPresignUploadsValidatesAllBeforeSigning(t *testing.T) {
	st := &fakeStorage{}
	svc := NewFileService(st)

	res := svc.PresignUploads(context.Background(), &dtos.PresignUploadsRequest{
		Files: []*dtos.PresignUploadFile{
			{File: newFileHeader(t, "tot.pdf", pdfContent)},
			{File: newFileHeader(t, "...", pdfContent)},
		},
	})
	if res.Err == nil {
		t.Fatal("expected error for invalid filename in batch")
	}
	if st.calls != 0 {
		t.Errorf("không được ký file nào khi batch còn file sai, storage called %d times", st.calls)
	}
	var e interface{ GetHttpCode() int }
	if !errors.As(res.Err, &e) || e.GetHttpCode() != http.StatusBadRequest {
		t.Errorf("expected 400 client error, got %v", res.Err)
	}
}

func TestPresignUploadsRejectsContentMismatch(t *testing.T) {
	st := &fakeStorage{}
	svc := NewFileService(st)

	res := svc.PresignUploads(context.Background(), &dtos.PresignUploadsRequest{
		Files: []*dtos.PresignUploadFile{
			{File: newFileHeader(t, "tot.pdf", pdfContent)},
			{File: newFileHeader(t, "doc.pdf", exeContent)},
		},
	})
	if res.Err == nil {
		t.Fatal("expected error for mismatched content in batch")
	}
	if st.calls != 0 {
		t.Errorf("không được ký file nào khi batch còn file sai nội dung, storage called %d times", st.calls)
	}
}

func TestPresignUploadsRejectsTooManyFiles(t *testing.T) {
	st := &fakeStorage{}
	svc := NewFileService(st)

	files := make([]*dtos.PresignUploadFile, 0, maxUploadsPerRequest+1)
	for i := 0; i <= maxUploadsPerRequest; i++ {
		files = append(files, &dtos.PresignUploadFile{File: newFileHeader(t, "f.pdf", pdfContent)})
	}
	res := svc.PresignUploads(context.Background(), &dtos.PresignUploadsRequest{Files: files})
	if res.Err == nil {
		t.Fatal("expected error for batch over the limit")
	}
	if st.calls != 0 {
		t.Errorf("không được ký file nào khi batch quá giới hạn, storage called %d times", st.calls)
	}
}

func TestResolveUploadBlocksUnallowedExtension(t *testing.T) {
	st := &fakeStorage{}
	svc := NewFileService(st)

	for _, name := range []string{"tool.exe", "script.sh", "index.php", "noext", "whatever.xyz"} {
		res := svc.PresignUpload(context.Background(), &dtos.PresignUploadRequest{
			File: newFileHeader(t, name, txtContent),
		})
		if res.Err == nil {
			t.Errorf("filename %q phải bị chặn", name)
			continue
		}
		var e interface{ GetHttpCode() int }
		if !errors.As(res.Err, &e) || e.GetHttpCode() != http.StatusBadRequest {
			t.Errorf("filename %q: expected 400, got %v", name, res.Err)
		}
	}
	if st.calls != 0 {
		t.Errorf("không được ký URL cho file ngoài whitelist, storage called %d times", st.calls)
	}

	if _, _, err := resolveUpload("bao-cao.pdf"); err != nil {
		t.Errorf("resolveUpload(.pdf) bị chặn sai: %v", err)
	}
}
