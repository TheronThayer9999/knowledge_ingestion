package extractor

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"knowledge_ingestion/src/common/utils"
)

// CỔNG ĐỌC TÀI LIỆU — chỗ duy nhất phải chạm khi đổi engine hoặc tách ra service riêng.
//
// Mỗi ĐỊNH DẠNG là một Reader độc lập (1 reader = 1 "tool đọc"). Pipeline
// (worker, article_pages, chunker, Qdrant) chỉ biết `Registry.Read`, không biết
// reader nào đang đọc hay đọc bằng thư viện gì.
//
// ĐỔI SANG THƯ VIỆN (ví dụ excelize — không thêm dep bây giờ, chỉ là hình dung):
//
//	// readers.go: đổi 1 dòng trong defaultReaders()
//	func defaultReaders() []Reader {
//		return []Reader{textReader{}, docxReader{}, excelizeReader{}, pptxReader{}, pdfReader{}, imageReader{}}
//	}
//
//	// xlsx_excelize.go: chỉ cần thêm file này, không sửa pipeline
//	type excelizeReader struct{}
//	func (excelizeReader) Info() ReaderInfo { return ReaderInfo{Name: "read_xlsx", Exts: []string{".xlsx"}, ...} }
//	func (excelizeReader) Read(ctx context.Context, req ReadRequest, visit func(Page) error) error {
//		f, err := excelize.OpenReader(req.Data) // archive cần seek: đọc hết vào RAM
//		...
//		return visitPieces(0, text, visit)
//	}
//
// VÌ SAO KHÔNG DỰNG MCP SERVER: bản thân app ĐÃ là agent (Eino in-process),
// nên bề mặt "tool" cho LLM là Eino tool (list_sheets/read_sheet trong
// src/agent/eino) chứ không phải server riêng — thêm process + transport chỉ
// đổi lấy cùng một chức năng. Cái cần tách là PHẦN ĐỌC (engine nặng, dễ thay),
// và nó đã tách sẵn sau interface Reader.
//
// Nếu sau này thật sự cần chạy khác process thì bề mặt đã đúng hình dạng MCP:
// Registry.Tools() ↔ tools/list (name, description, exts), Registry.Read(ReadRequest,
// visit) ↔ tools/call, Page đẩy qua visit ↔ [{num, seq, text}]. Vì vậy
// ReadRequest cố ý KHÔNG chứa gì thuộc queue/DB — chỉ dữ liệu + tuỳ chọn đọc, để
// bên kia process nhận đúng payload đó (thay reader bằng ReaderFunc gọi client).

// Reader là 1 adapter đọc 1 định dạng tài liệu.
type Reader interface {
	// Info mô tả tool: tên duy nhất, mô tả cho người/LLM đọc, đuôi nhận.
	Info() ReaderInfo
	// Read trích TEXT THÔ rồi đẩy từng mảnh qua visit. Hợp đồng giống
	// ExtractRawPages: visit lỗi thì dừng và trả lỗi đó, không gọi visit song
	// song, có thể trả ErrUnsupported/ErrInvalid (xem IsPermanent).
	Read(ctx context.Context, req ReadRequest, visit func(Page) error) error
}

// ReaderInfo là mô tả công khai của 1 reader — cùng hình dạng với descriptor
// tool MCP để sau này `tools/list` chỉ việc trả thẳng.
type ReaderInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Exts        []string `json:"exts"`
}

// ReadRequest là "arguments" của một lượt đọc.
//
// Key là tên blob (để lấy đuôi + log), KHÔNG phải đường dẫn đọc được. Data là
// nội dung thật — reader tự quyết đọc bao nhiêu (có trần riêng từng loại).
type ReadRequest struct {
	Key  string
	Data io.Reader
	// OCR chỉ reader ảnh/PDF dùng; OCR tắt thì read_image trả ErrUnsupported.
	OCR OCRConfig
	// Layout chỉ read_pdf dùng (đo bố cục phức tạp, xem WithLayoutDetection).
	Layout bool
	// Have là tập seq đã trích lần trước — chỉ read_pdf dùng để resume.
	Have map[int]bool
}

// ReaderFunc biến một hàm thành Reader — đủ cho adapter thư viện (excelize,
// unioffice, pdfcpu) hoặc client MCP, khỏi phải khai struct rỗng.
type ReaderFunc struct {
	info ReaderInfo
	read func(ctx context.Context, req ReadRequest, visit func(Page) error) error
}

// NewReaderFunc dựng Reader từ hàm đọc.
func NewReaderFunc(info ReaderInfo, read func(context.Context, ReadRequest, func(Page) error) error) ReaderFunc {
	return ReaderFunc{info: info, read: read}
}

func (f ReaderFunc) Info() ReaderInfo { return f.info }

func (f ReaderFunc) Read(ctx context.Context, req ReadRequest, visit func(Page) error) error {
	if f.read == nil {
		return fmt.Errorf("reader %q chưa có hàm Read", f.info.Name)
	}
	return f.read(ctx, req, visit)
}

// Registry ánh xạ đuôi file → reader và tên tool → reader.
//
// Bất biến sau khi dựng: thêm/ghi đè reader phải qua With (trả BẢN SAO) — nhờ
// vậy test cắm adapter giả không làm bẩn registry mặc định, và không có trạng
// thái toàn cục mutable.
type Registry struct {
	byExt  map[string]Reader
	byName map[string]Reader
	tools  []ReaderInfo
}

// NewRegistry dựng registry từ danh sách reader. Reader ĐĂNG KÝ SAU THẮNG khi
// trùng đuôi — đó là cơ chế cắm thư viện thay stdlib mà không sửa dispatch.
func NewRegistry(readers ...Reader) *Registry {
	r := &Registry{byExt: make(map[string]Reader), byName: make(map[string]Reader)}
	r.register(readers...)
	return r
}

func (r *Registry) register(readers ...Reader) {
	for _, rd := range readers {
		if rd == nil {
			continue
		}
		info := rd.Info()
		exts := make([]string, 0, len(info.Exts))
		for _, ext := range info.Exts {
			norm := normalizeExt(ext)
			if norm == "" {
				continue
			}
			r.byExt[norm] = rd
			exts = append(exts, norm)
		}
		info.Exts = exts
		// Cùng tên tool xuất hiện lần nữa = ghi đè có chủ ý (With), ví dụ đổi
		// read_xlsx sang bản excelize — bỏ descriptor cũ để Tools() không liệt
		// kê hai lần cùng tên. Không so sánh reader vì ReaderFunc chứa func
		// (không so sánh được) sẽ panic khi so sánh interface.
		if _, ok := r.byName[info.Name]; ok {
			r.removeTool(info.Name)
		}
		r.byName[info.Name] = rd
		r.tools = append(r.tools, info)
	}
}

// removeTool bỏ descriptor cũ để Tools() không liệt kê 2 lần cùng tên.
func (r *Registry) removeTool(name string) {
	for i, tl := range r.tools {
		if tl.Name == name {
			r.tools = slices.Delete(r.tools, i, i+1)
			return
		}
	}
}

// With trả bản sao có thêm/ghi đè reader. Dùng khi cần cắm adapter thư viện cho
// một test hay một tiến trình mà không đổi registry mặc định.
func (r *Registry) With(readers ...Reader) *Registry {
	next := &Registry{
		byExt:  make(map[string]Reader, len(r.byExt)+len(readers)),
		byName: make(map[string]Reader, len(r.byName)+len(readers)),
		tools:  make([]ReaderInfo, len(r.tools), len(r.tools)+len(readers)),
	}
	for ext, rd := range r.byExt {
		next.byExt[ext] = rd
	}
	for name, rd := range r.byName {
		next.byName[name] = rd
	}
	copy(next.tools, r.tools)
	next.register(readers...)
	return next
}

// ReaderFor tìm reader theo đuôi của key/tên file hoặc theo chính đuôi
// (".xlsx", "XLSX", "uploads/a.XLSX" đều đúng). Đuôi đã có dot thì dùng thẳng;
// chuỗi trơn như "xlsx" thử thành ".xlsx" trước (kiểu arguments của tool chỉ
// gửi ext), không khớp mới tách bằng utils.FileExt.
func (r *Registry) ReaderFor(keyOrExt string) (Reader, bool) {
	ext := strings.ToLower(strings.TrimSpace(keyOrExt))
	if ext == "" {
		return nil, false
	}
	if !strings.HasPrefix(ext, ".") {
		// Chỉ thử ".<token>" khi token không phải đường dẫn — tránh dựng key
		// rác từ "a/b" rồi bỏ sót tên file thật.
		if !strings.ContainsAny(ext, "/\\") {
			if rd, ok := r.byExt["."+ext]; ok {
				return rd, true
			}
		}
		ext = utils.FileExt(ext)
	}
	rd, ok := r.byExt[ext]
	return rd, ok
}

// Tool tìm reader theo TÊN tool (như tools/call của MCP).
func (r *Registry) Tool(name string) (Reader, bool) {
	rd, ok := r.byName[name]
	return rd, ok
}

// Supports báo registry có reader cho loại file này không.
func (r *Registry) Supports(key string) bool {
	_, ok := r.ReaderFor(key)
	return ok
}

// Exts trả các đuôi đọc được, sắp tăng dần (test/CLI so với whitelist upload).
func (r *Registry) Exts() []string {
	exts := make([]string, 0, len(r.byExt))
	for ext := range r.byExt {
		exts = append(exts, ext)
	}
	slices.Sort(exts)
	return exts
}

// Tools liệt kê descriptor của mọi reader — bề mặt `tools/list` của MCP.
func (r *Registry) Tools() []ReaderInfo {
	out := make([]ReaderInfo, len(r.tools))
	for i, tl := range r.tools {
		out[i] = tl
		out[i].Exts = slices.Clone(tl.Exts)
	}
	return out
}

// Read định tuyến theo ĐUÔI của req.Key rồi gọi reader. Đây là hàm pipeline
// dùng, và cũng là thứ một MCP server (tools/call) chỉ cần gọi lại.
func (r *Registry) Read(ctx context.Context, req ReadRequest, visit func(Page) error) error {
	rd, ok := r.ReaderFor(req.Key)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnsupported, utils.FileExt(req.Key))
	}
	return rd.Read(ctx, req, visit)
}

// normalizeExt chuẩn hoá đuôi reader khai báo: viết thường, có dấu chấm đầu.
// Chỉ nhận đuôi có nghĩa — khai báo sai kiểu "xlsx,xls" thì bỏ qua thay vì tạo
// một key rác không bao giờ khớp.
func normalizeExt(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	if strings.ContainsAny(ext, " ,;") || ext == "." {
		return ""
	}
	return ext
}
