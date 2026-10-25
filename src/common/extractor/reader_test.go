package extractor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

// fakeReader thay engine của MỘT định dạng — đúng kiểu cắm excelize/unioffice
// sau này: chỉ thêm reader + đăng ký, pipeline không biết gì.
type fakeReader struct {
	info  ReaderInfo
	pages []Page
	calls int
}

func (f *fakeReader) Info() ReaderInfo { return f.info }

func (f *fakeReader) Read(_ context.Context, _ ReadRequest, visit func(Page) error) error {
	f.calls++
	for _, p := range f.pages {
		if err := visit(p); err != nil {
			return err
		}
	}
	return nil
}

// readWith đọc qua MỘT registry (mặc định hoặc bản sao With) — ExtractPages
// luôn dùng defaultRegistry nên test cắm adapter phải gọi registry trực tiếp.
func readWith(t *testing.T, reg *Registry, key string, r io.Reader) []Page {
	t.Helper()
	var pages []Page
	err := reg.Read(context.Background(), ReadRequest{Key: key, Data: r}, func(p Page) error {
		pages = append(pages, p)
		return nil
	})
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	return pages
}

// TestDefaultRegistryToolsWellFormed khoá bề mặt tools/list: tên duy nhất,
// mô tả có thật (LLM đọc nó để chọn tool), đuôi viết thường có dấu chấm, và
// mỗi đuôi chỉ thuộc đúng 1 tool — 2 tool nhận cùng đuôi là dispatch mơ hồ.
func TestDefaultRegistryToolsWellFormed(t *testing.T) {
	tools := DefaultRegistry().Tools()
	if len(tools) == 0 {
		t.Fatal("registry rỗng")
	}
	names := make(map[string]bool, len(tools))
	owner := make(map[string]string)
	for _, tl := range tools {
		if tl.Name == "" || tl.Description == "" {
			t.Fatalf("tool thiếu tên/mô tả: %+v", tl)
		}
		if names[tl.Name] {
			t.Fatalf("tên tool trùng: %q", tl.Name)
		}
		names[tl.Name] = true
		if len(tl.Exts) == 0 {
			t.Fatalf("tool %q không khai báo đuôi nào", tl.Name)
		}
		for _, ext := range tl.Exts {
			if ext != strings.ToLower(ext) || !strings.HasPrefix(ext, ".") {
				t.Fatalf("tool %q khai báo đuôi %q chưa chuẩn hoá", tl.Name, ext)
			}
			if prev, ok := owner[ext]; ok {
				t.Fatalf("đuôi %q thuộc cả %q và %q", ext, prev, tl.Name)
			}
			owner[ext] = tl.Name
		}
	}
	want := make([]string, 0, len(owner))
	for ext := range owner {
		want = append(want, ext)
	}
	slices.Sort(want)
	if got := DefaultRegistry().Exts(); !slices.Equal(got, want) {
		t.Fatalf("Exts() = %v, hợp các đuôi khai báo = %v", got, want)
	}
	if DefaultRegistry().Supports("uploads/a.zip") {
		t.Fatal(".zip là archive, không được báo hỗ trợ")
	}
}

// TestRegistryReadRoutesByExt chứng minh dispatch dựa trên đuôi chứ không
// phải switch cứng: 3 định dạng khác nhau đi qua cùng một hàm Read.
func TestRegistryReadRoutesByExt(t *testing.T) {
	reg := DefaultRegistry()

	txt := readWith(t, reg, "uploads/a.txt", strings.NewReader("xin chào"))
	if len(txt) != 1 || txt[0].Text != "xin chào" {
		t.Fatalf("read_text sai: %+v", txt)
	}

	xlsx := readWith(t, reg, "uploads/a.xlsx", bytes.NewReader(buildXlsx(t)))
	if len(xlsx) == 0 || !strings.Contains(xlsx[0].Text, "## Sheet: Biểu phí") {
		t.Fatalf("read_xlsx sai: %+v", xlsx)
	}

	docx := readWith(t, reg, "uploads/a.docx", bytes.NewReader(buildDocx(t, `<?xml version="1.0"?><w:document `+docxNS+`><w:body>`+
		`<w:p><w:r><w:t>Điều 1. Phạm vi</w:t></w:r></w:p></w:body></w:document>`)))
	if len(docx) != 1 || !strings.Contains(docx[0].Text, "Điều 1. Phạm vi") {
		t.Fatalf("read_docx sai: %+v", docx)
	}

	err := reg.Read(context.Background(), ReadRequest{Key: "uploads/a.zip", Data: strings.NewReader("")}, nil)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("đuôi lạ phải là ErrUnsupported, got %v", err)
	}
}

// TestRegistryWithOverridesOnlyItsExt là kịch bản đổi thư viện: cắm reader
// mới cho .xlsx, .docx vẫn về stdlib, và registry mặc định KHÔNG bị bẩn.
func TestRegistryWithOverridesOnlyItsExt(t *testing.T) {
	fake := &fakeReader{
		info:  ReaderInfo{Name: "read_xlsx", Description: "bản giả", Exts: []string{".xlsx"}},
		pages: []Page{{Seq: 1, Text: "nội dung từ thư viện khác"}},
	}
	reg := DefaultRegistry().With(fake)

	got := readWith(t, reg, "uploads/a.xlsx", strings.NewReader(""))
	if len(got) != 1 || got[0].Text != "nội dung từ thư viện khác" {
		t.Fatalf("chưa dùng reader mới: %+v", got)
	}
	if fake.calls != 1 {
		t.Fatalf("reader mới được gọi %d lần, want 1", fake.calls)
	}

	// Định dạng khác không bị ảnh hưởng.
	docx := readWith(t, reg, "uploads/a.docx", bytes.NewReader(buildDocx(t, `<?xml version="1.0"?><w:document `+docxNS+`><w:body>`+
		`<w:p><w:r><w:t>Điều 2</w:t></w:r></w:p></w:body></w:document>`)))
	if len(docx) != 1 || !strings.Contains(docx[0].Text, "Điều 2") {
		t.Fatalf("read_docx phải giữ reader cũ: %+v", docx)
	}

	// Tools() không được liệt kê read_xlsx 2 lần sau khi ghi đè.
	count := 0
	for _, tl := range reg.Tools() {
		if tl.Name == "read_xlsx" {
			count++
			if tl.Description != "bản giả" {
				t.Fatalf("descriptor cũ còn sót: %+v", tl)
			}
		}
	}
	if count != 1 {
		t.Fatalf("read_xlsx xuất hiện %d lần, want 1", count)
	}

	// Registry mặc định là bất biến: bản sao không làm nó đổi.
	rd, ok := DefaultRegistry().ReaderFor(".xlsx")
	if !ok {
		t.Fatal("mặc định mất read_xlsx")
	}
	if _, isFake := rd.(*fakeReader); isFake {
		t.Fatal("With đã làm bẩn registry mặc định")
	}
}

// TestRegistryReaderForAcceptsKeyOrExt: LLM/CLI hay truyền tên file, ".XLSX"
// hoa, hoặc chỉ đuôi — tất cả phải về cùng reader.
func TestRegistryReaderForAcceptsKeyOrExt(t *testing.T) {
	want, ok := DefaultRegistry().ReaderFor(".xlsx")
	if !ok {
		t.Fatal("mặc định thiếu .xlsx")
	}
	for _, sel := range []string{".xlsx", "XLSX", "uploads/thang_5.XLSX", "  .xlsx  "} {
		rd, found := DefaultRegistry().ReaderFor(sel)
		if !found || rd != want {
			t.Fatalf("ReaderFor(%q) không khớp .xlsx", sel)
		}
	}
	if _, ok := DefaultRegistry().ReaderFor(""); ok {
		t.Fatal("chuỗi rỗng không được khớp reader nào")
	}
}

// TestRegistryNormalizesDeclaredExts: khai báo bẩn ("XLSX", "xlsx,xls", "")
// chỉ được nhận đuôi có nghĩa — tránh key rác không bao giờ khớp.
func TestRegistryNormalizesDeclaredExts(t *testing.T) {
	reg := NewRegistry(NewReaderFunc(ReaderInfo{
		Name: "read_bay",
		Exts: []string{"XLSX", "xlsx,xls", "", "."},
	}, func(context.Context, ReadRequest, func(Page) error) error { return nil }))
	if got := reg.Exts(); !slices.Equal(got, []string{".xlsx"}) {
		t.Fatalf("Exts() = %v, want [.xlsx]", got)
	}
}

// TestReaderFuncWithoutRead: adapter dựng thiếu hàm đọc phải báo lỗi rõ ràng
// chứ không panic nil pointer.
func TestReaderFuncWithoutRead(t *testing.T) {
	rd := NewReaderFunc(ReaderInfo{Name: "read_missing", Exts: []string{".abc"}}, nil)
	err := rd.Read(context.Background(), ReadRequest{Key: "a.abc"}, func(Page) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "chưa có hàm Read") {
		t.Fatalf("want lỗi thiếu hàm Read, got %v", err)
	}
}
