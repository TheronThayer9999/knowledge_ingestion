package utils

import (
	"bytes"
	"mime/multipart"
	"strings"
	"testing"
)

func TestSanitizeFileName(t *testing.T) {
	cases := map[string]string{
		"bao-cao.pdf":    "bao-cao.pdf",
		"Báo Cáo.PDF":    "B_o_C_o.PDF",
		"a/b\\c.pdf":     "a_b_c.pdf",
		"sp ace,x.pdf":   "sp_ace_x.pdf",
		"":               "",
		"giữ-số_01.txt": "gi_-s__01.txt",
	}
	for in, want := range cases {
		if got := SanitizeFileName(in); got != want {
			t.Errorf("SanitizeFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFileExt(t *testing.T) {
	if got := FileExt("bao-cao.pdf"); got != ".pdf" {
		t.Errorf("FileExt = %q, want .pdf", got)
	}
	if got := FileExt("Báo Cáo.PDF"); got != ".pdf" {
		t.Errorf("FileExt uppercase = %q, want .pdf", got)
	}
	for _, name := range []string{"noext", "...", ".hidden", "a.", "x.quaidaronbaomongquylong"} {
		if got := FileExt(name); got != "" {
			t.Errorf("FileExt(%q) = %q, want empty", name, got)
		}
	}
	if got := FileExt("archive.tar.gz"); got != ".gz" {
		t.Errorf("FileExt multi-dot = %q, want .gz", got)
	}
}

func TestSniffContentType(t *testing.T) {
	if got := SniffContentType([]byte("%PDF-1.4\n")); got != "application/pdf" {
		t.Errorf("pdf sniff = %q", got)
	}
	// text luôn kèm charset — SniffContentType phải cắt bỏ.
	if got := SniffContentType([]byte("hello plain text")); got != "text/plain" {
		t.Errorf("text sniff = %q, want text/plain", got)
	}
	// MZ + byte NUL như file .exe thật mới bị coi là binary.
	if got := SniffContentType([]byte{'M', 'Z', 0x90, 0x00, 0x03, 0x00}); got != "application/octet-stream" {
		t.Errorf("exe sniff = %q, want application/octet-stream", got)
	}
}

func TestReadPartHeadCapsAtSniffHeadLen(t *testing.T) {
	big := bytes.Repeat([]byte("a"), SniffHeadLen+100)
	fh := newTestFileHeader(t, "big.txt", big)
	head, err := ReadPartHead(fh)
	if err != nil {
		t.Fatalf("ReadPartHead error: %v", err)
	}
	if len(head) != SniffHeadLen {
		t.Errorf("len(head) = %d, want %d", len(head), SniffHeadLen)
	}
}

func TestReadPartHeadKeepsSmallFile(t *testing.T) {
	small := []byte("tiny")
	fh := newTestFileHeader(t, "tiny.txt", small)
	head, err := ReadPartHead(fh)
	if err != nil {
		t.Fatalf("ReadPartHead error: %v", err)
	}
	if !bytes.Equal(head, small) {
		t.Errorf("head = %q, want %q", head, small)
	}
}

func newTestFileHeader(t *testing.T, filename string, content []byte) *multipart.FileHeader {
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

func TestNewUUIDv7(t *testing.T) {
	a, err := NewUUIDv7()
	if err != nil {
		t.Fatalf("NewUUIDv7 error: %v", err)
	}
	b, err := NewUUIDv7()
	if err != nil {
		t.Fatalf("NewUUIDv7 error: %v", err)
	}
	if a == b {
		t.Errorf("two UUIDs are identical: %q", a)
	}
	// UUIDv7 dạng 8-4-4-4-12, version nibble = 7.
	parts := strings.Split(a, "-")
	if len(parts) != 5 || len(parts[2]) != 4 || parts[2][0] != '7' {
		t.Errorf("UUID %q is not v7 format", a)
	}
}
