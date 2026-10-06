package apis

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/services"

	"github.com/gin-gonic/gin"
)

// stubFileService trả kết quả đóng dấu, chỉ ghi nhận thứ handler bóc được.
type stubFileService struct {
	lastSingle *dtos.PresignUploadRequest
	lastBatch  *dtos.PresignUploadsRequest
}

func (s *stubFileService) PresignUpload(_ context.Context, req *dtos.PresignUploadRequest) dtos.Result[*dtos.PresignUploadResponse] {
	s.lastSingle = req
	return dtos.Ok(&dtos.PresignUploadResponse{
		UploadURL: "http://storage/k", Key: "k", Method: http.MethodPut,
		ContentType: "application/pdf", ExpiresAt: time.Now(),
	})
}

func (s *stubFileService) PresignUploads(_ context.Context, req *dtos.PresignUploadsRequest) dtos.Result[*dtos.PresignUploadsResponse] {
	s.lastBatch = req
	items := make([]dtos.PresignUploadsItem, 0, len(req.Files))
	for _, f := range req.Files {
		items = append(items, dtos.PresignUploadsItem{Filename: f.Filename, Key: "k", Method: http.MethodPut})
	}
	return dtos.Ok(&dtos.PresignUploadsResponse{Items: items})
}

var _ services.IFileService = (*stubFileService)(nil)

func newFileAPI() (*gin.Engine, *stubFileService) {
	gin.SetMode(gin.TestMode)
	stub := &stubFileService{}
	api := NewFileAPI(newRenderer(), stub)
	r := gin.New()
	r.POST("/presign", api.PresignUpload)
	r.POST("/presign/batch", api.PresignUploads)
	return r, stub
}

type testFile struct {
	name    string
	content []byte
}

func multipartBody(t *testing.T, field string, files []testFile) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, f := range files {
		part, err := w.CreateFormFile(field, f.name)
		if err != nil {
			t.Fatalf("CreateFormFile: %v", err)
		}
		if _, err := part.Write(f.content); err != nil {
			t.Fatalf("write part: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &buf, w.FormDataContentType()
}

// countMultipartTemp đếm file tạm Go buffer khi ParseMultipartForm (>32MB).
func countMultipartTemp(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatalf("ReadDir temp: %v", err)
	}
	n := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "multipart-") {
			n++
		}
	}
	return n
}

func TestPresignUploadStreamsLargeFileWithoutTempFiles(t *testing.T) {
	const size = 40 << 20 // 40MB — vượt ngưỡng 32MB gin buffer ra đĩa
	content := make([]byte, size)
	copy(content, "%PDF-1.4\n")

	r, stub := newFileAPI()
	body, ctype := multipartBody(t, "file", []testFile{{name: "big.pdf", content: content}})

	before := countMultipartTemp(t)
	req := httptest.NewRequest(http.MethodPost, "/presign", body)
	req.Header.Set("Content-Type", ctype)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if stub.lastSingle == nil || stub.lastSingle.Filename != "big.pdf" {
		t.Fatalf("service không nhận đúng filename: %+v", stub.lastSingle)
	}
	if len(stub.lastSingle.Head) == 0 || len(stub.lastSingle.Head) > 512 {
		t.Fatalf("head phải 1..512 bytes, got %d", len(stub.lastSingle.Head))
	}
	if after := countMultipartTemp(t); after != before {
		t.Errorf("handler để lại %d file tạm multipart-* (streaming không được sinh file)", after-before)
	}
}

func TestPresignUploadsKeepsOrder(t *testing.T) {
	r, stub := newFileAPI()
	body, ctype := multipartBody(t, "files", []testFile{
		{name: "a.pdf", content: []byte("%PDF-1.4\n")},
		{name: "b.txt", content: []byte("hello\n")},
	})
	req := httptest.NewRequest(http.MethodPost, "/presign/batch", body)
	req.Header.Set("Content-Type", ctype)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(stub.lastBatch.Files) != 2 {
		t.Fatalf("got %d files, want 2", len(stub.lastBatch.Files))
	}
	if stub.lastBatch.Files[0].Filename != "a.pdf" || stub.lastBatch.Files[1].Filename != "b.txt" {
		t.Errorf("thứ tự file sai: %q, %q", stub.lastBatch.Files[0].Filename, stub.lastBatch.Files[1].Filename)
	}
}

func TestPresignUploadNoFileIsBadRequest(t *testing.T) {
	r, _ := newFileAPI()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("note", "no file here")
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/presign", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}
