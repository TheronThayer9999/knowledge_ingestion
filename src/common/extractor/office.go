package extractor

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Trích .xlsx/.pptx dùng chung phần "mở archive": cả họ Office Open XML đều
// là zip chứa XML, y hệt .docx (xem docx.go). Gom helper ở đây để 2 file kia
// chỉ còn việc đọc đúng entry — không lặp lại phần kiểm trần/zip-slip.

// openOfficeZip đọc CẢ archive vào RAM rồi mở. Zip cần seek nên không stream
// được, vì vậy đây là giới hạn RAM thật chứ không phải lựa chọn — trần dùng
// chung maxDocxArchiveBytes (100MB zip đã là tài liệu khổng lồ).
func openOfficeZip(r io.Reader, kind string) (*zip.Reader, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxDocxArchiveBytes+1))
	if err != nil {
		return nil, fmt.Errorf("đọc file %s: %w", kind, err)
	}
	if len(raw) > maxDocxArchiveBytes {
		return nil, fmt.Errorf("%w: file %s quá lớn (%d bytes)", ErrInvalid, kind, len(raw))
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("%w: file %s không hợp lệ: %v", ErrInvalid, kind, err)
	}
	return zr, nil
}

// zipEntryReader mở 1 entry để đọc XML. Caller phải Close.
func zipEntryReader(f *zip.File) (io.ReadCloser, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: mở %s: %v", ErrInvalid, f.Name, err)
	}
	return rc, nil
}

// findZipEntry trả entry theo tên chính xác, nil nếu không có.
func findZipEntry(zr *zip.Reader, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// numberedFile là 1 entry dạng "<prefix><số>.xml" — pptx đặt slide1..slideN,
// xlsx đặt sheet1..sheetN. Giữ số để sắp theo SỐ chứ không theo chuỗi
// (slide10 phải sau slide2, sắp chuỗi thì slide10 đứng ngay sau slide1).
type numberedFile struct {
	num  int
	file *zip.File
}

// readZipEntry đọc TRỌN 1 entry XML vào RAM, có trần. Header zip có thể khai
// gian size nên luôn qua LimitReader thay vì tin UncompressedSize64.
func readZipEntry(f *zip.File) ([]byte, error) {
	rc, err := zipEntryReader(f)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(io.LimitReader(rc, maxDocxArchiveBytes))
	if err != nil {
		return nil, fmt.Errorf("đọc %s: %w", f.Name, err)
	}
	return data, nil
}

// numberedEntries lọc entry khớp "<prefix><số>.xml" và sắp tăng dần theo số.
func numberedEntries(zr *zip.Reader, prefix string) []numberedFile {
	var out []numberedFile
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, prefix) || !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		mid := strings.TrimSuffix(strings.TrimPrefix(f.Name, prefix), ".xml")
		// Chỉ nhận phần giữa là số thuần — "slideLayout1.xml" hay
		// "sheet1.xml.bak" không được coi là slide/sheet.
		n, err := strconv.Atoi(mid)
		if err != nil || n <= 0 {
			continue
		}
		out = append(out, numberedFile{num: n, file: f})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].num < out[j].num })
	return out
}
