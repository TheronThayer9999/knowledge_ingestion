package constants

import "time"

// Giới hạn upload file tập trung ở đây để tường minh — mọi tầng dùng chung,
// đổi một chỗ là đủ. Đặt tên UPPER_CASE theo quy ước const dùng chung.

// PRESIGN_EXPIRY là thời hạn sống của URL upload — hết hạn client phải xin lại.
const PRESIGN_EXPIRY = 15 * time.Minute

// UPLOAD_PREFIX là "thư mục ảo" trong bucket, để file upload không trộn với file khác.
const UPLOAD_PREFIX = "uploads/"

// MAX_FILENAME_LEN chốt độ dài tên file, tránh key quá to.
const MAX_FILENAME_LEN = 100

// MAX_UPLOADS_PER_REQUEST chốt số file tối đa mỗi request batch. Check tay ở
// service (và handler chặn sớm khi stream) vì tag binding max=20 trên DTO
// không chạy qua đường multipart (validator dùng tag "validate", gin binding
// mới đọc tag "binding").
const MAX_UPLOADS_PER_REQUEST = 20

// MAX_FILE_SIZE chốt dung lượng tối đa mỗi file được presign — 500MB.
// Handler đo size thật trên stream rồi điền vào DTO, service chặn ở đây để
// không tin con số client tự khai.
const MAX_FILE_SIZE = 500 << 20

// MAX_STREAM_PARTS chặn số part tối đa lướt qua khi đọc multipart stream —
// client gửi vô hạn part (field rác, file rác) thì 400 sớm thay vì loop
// treo connection. 20 file + dư địa cho field phụ.
const MAX_STREAM_PARTS = 64

// EXTRACTED_PREFIX là "thư mục ảo" trong bucket cho artifact text đã trích
// (phase extract) — tách khỏi UPLOAD_PREFIX để dọn/lifecycle nhìn 2 nhóm
// riêng. Key artifact suy ra TẤT ĐỊNH từ storage key gốc nên chạy lại ghi
// đè đúng object cũ, không bao giờ sinh mồ côi.
const EXTRACTED_PREFIX = "extracted/"

// Knob vận hành phase extract của worker — service chỉ đọc, không định nghĩa.
//
// Extract là phase ĐẦU: bóc text từ blob (OCR nếu là PDF scan) rồi ghi thành
// artifact trên S3. Tách riêng khỏi chunk để đổi cấu hình chunker/size chỉ
// chạy lại phase chunk — không phải OCR lại cả file.

// EXTRACT_INTERVAL là nhịp worker quét bài chưa trích text.
const EXTRACT_INTERVAL = 30 * time.Second

// EXTRACT_CLAIM_LIMIT số bài hốt mỗi kỳ — claim là rẻ (1 transaction), làm
// không hết thì lease giữ bài lại, kỳ sau hốt tiếp.
const EXTRACT_CLAIM_LIMIT = 6

// EXTRACT_POOL_SIZE số bài trích song song. Để 1: gopdf nạp cả PDF vào RAM
// (đo: PDF 90MB đỉnh ~920MB), 3 job song song là ~3GB chỉ riêng extract.
// Trích tuần tự thì nhanh đủ, vì bottleneck thật là index.
const EXTRACT_POOL_SIZE = 1

// EXTRACT_PAGE_WORKERS số khoảng trang của MỘT PDF trích song song (mỗi khoảng
// là 1 tiến trình pdftotext, trang scan thêm render + OCR). Tổng tiến trình
// poppler/OCR đồng thời = EXTRACT_PAGE_WORKERS, vì EXTRACT_POOL_SIZE = 1.
const EXTRACT_PAGE_WORKERS = 4

// EXTRACT_LEASE thời gian giữ claim — phải đủ cho file nặng nhất (PDF scan
// 500 trang ≈ 17 phút).
const EXTRACT_LEASE = 30 * time.Minute

// EXTRACT_FLUSH_PAGES số trang ghi 1 lô ra artifact DỞ. Job chết giữa file
// 500 trang thì kỳ sau nhảy tiếp từ trang kế tiếp thay vì OCR lại từ đầu.
// Đổi số này là đánh đổi giữa an toàn và round trip — 20 trang ≈ 40 giây OCR
// bị làm lại, đợt 500 trang chỉ 25 lần PUT.
const EXTRACT_FLUSH_PAGES = 20

// EXTRACT_JOB_TIMEOUT trần thời gian 1 kỳ quét extract — lớn hơn
// EXTRACT_LEASE một chút, xem INDEX_JOB_TIMEOUT để biết lý do chọn con số.
const EXTRACT_JOB_TIMEOUT = 45 * time.Minute

// Knob vận hành phase index của worker — service chỉ đọc, không định nghĩa.
//
// Index là phase CUỐI: cắt chunk từ toàn văn bản đã trích rồi embed + upsert
// Qdrant, làm chung một lượt (xem IArticleIndexService để biết vì sao gộp).

// INDEX_INTERVAL là nhịp worker quét bài chưa index — main lấy làm chu kỳ
// ticker. 10s để dev thấy kết quả nhanh; production tải cao thì nâng lên 1
// phút để đỡ quét DB trống.
const INDEX_INTERVAL = 10 * time.Second

// INDEX_CLAIM_LIMIT số bài hốt mỗi kỳ — ollama remote chậm, bài 500 trang
// (~1500 chunk ≈ 47 lần gọi) nuốt vài phút nên giữ ít.
const INDEX_CLAIM_LIMIT = 3

// INDEX_POOL_SIZE số bài index song song — ollama remote là bottleneck nên
// chỉ 2, tránh dội request làm nó nghẽn rồi timeout hàng loạt.
const INDEX_POOL_SIZE = 2

// INDEX_LEASE thời gian giữ claim — đủ cho bài to nhất (500 trang) cắt +
// embed + upsert xong trong pool; crash thì quá lease tự reclaim.
const INDEX_LEASE = 30 * time.Minute

// INDEX_BATCH_SIZE số text mỗi lần gọi ollama — khớp giới hạn input 1 lần
// gọi của /api/embed. Đợt này VỪA là đơn vị upsert VỪA là mốc ghi checkpoint,
// nên nó quyết định cả "mất bao nhiêu khi job chết" lẫn "ôm bao nhiêu RAM".
const INDEX_BATCH_SIZE = 32

// INDEX_PAGE_WINDOW số trang phase index nạp vào RAM mỗi lượt — đây là TRẦN
// BỘ NHỚ của cả phase, không phụ thuộc kích thước file. 8 trang ≈ 40KB text,
// đủ rộng để SplitBlocks thấy tiêu đề Điều/mục trong phạm vi vài trang và
// đủ hẹp để file 5000 trang không làm ngốc RAM.
//
// Cửa sổ LUÔN kết thúc ở ranh giới trang, nên không có trang nào bị đọc 2 lần
// và số trang của mỗi chunk vẫn chính xác tuyệt đối.
const INDEX_PAGE_WINDOW = 8

// INDEX_WINDOW_MAX_CHARS trần ký tự của 1 cửa sổ, chặn trường hợp loại file
// 1 trang (txt/docx gom cả file thành 1 Page) — nếu chỉ đếm trang thì cửa sổ
// đó có thể chứa cả 20MB. Quá ngưỡng thì cắt cửa sổ sớm.
const INDEX_WINDOW_MAX_CHARS = 64 << 10

// INDEX_CALL_ATTEMPTS số lần thử lại 1 lần gọi ollama/qdrant chập chờn,
// nghỉ backoff nhân đôi từ INDEX_CALL_BASE_DELAY.
const INDEX_CALL_ATTEMPTS = 3
const INDEX_CALL_BASE_DELAY = 2 * time.Second

// INDEX_JOB_TIMEOUT trần thời gian 1 kỳ quét index — PHẢI LỚN HƠN INDEX_LEASE
// một chút. Timeout ở đây chỉ là chốt an toàn cuối: nhờ checkpoint ghi sau
// mỗi INDEX_BATCH_SIZE nên hết giờ không còn là vòng lặp vô hạn, kỳ sau chỉ
// làm nốt phần còn lại.
const INDEX_JOB_TIMEOUT = 45 * time.Minute


// Knob vận hành phase purge của worker — service chỉ đọc, không định nghĩa.

// PURGE_GRACE_PERIOD chỉ dọn bài đã xóa mềm quá 5 phút — cửa sổ undo cho user
// đổi ý + tránh đua với request đọc ngay sau khi xóa. Main lấy làm chu kỳ
// ticker: 1 nguồn sự thật duy nhất, khỏi 2 hằng 5-phút ở 2 file lệch nhau.
const PURGE_GRACE_PERIOD = 5 * time.Minute

// PURGE_BATCH_SIZE số row xử lý mỗi kỳ quét — đủ nhỏ để 1 kỳ chạy xong nhanh,
// còn tồn thì kỳ sau dọn tiếp.
const PURGE_BATCH_SIZE = 100

// QDRANT_COLLECTION là collection duy nhất worker dùng — 1 collection cho mọi
// article, phân biệt bằng payload article_id (filter + xóa theo bài).
const QDRANT_COLLECTION = "article_chunks"

// SEARCH_DEFAULT_LIMIT số hit search mặc định — agent RAG chỉ cần vài đoạn
// gần nhất làm ngữ cảnh, nhiều hơn thì xin rõ limit.
const SEARCH_DEFAULT_LIMIT = 5

// SEARCH_MAX_LIMIT trần hit mỗi query search — chặn request vô hạn làm nặng
// Qdrant + response phình.
const SEARCH_MAX_LIMIT = 20

const (
	CONTENT_TYPE_PDF  = "application/pdf"
	CONTENT_TYPE_DOCX = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	CONTENT_TYPE_TXT  = "text/plain"
	CONTENT_TYPE_HTML = "text/html"
	CONTENT_TYPE_XML  = "application/xml"
	CONTENT_TYPE_JSON = "application/json"
	CONTENT_TYPE_ZIP  = "application/zip"
	CONTENT_TYPE_MD   = "text/markdown"
	CONTENT_TYPE_CSV  = "text/csv"
	CONTENT_TYPE_XLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

	// PowerPoint
	CONTENT_TYPE_PPTX = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	CONTENT_TYPE_PPT  = "application/vnd.ms-powerpoint"

	// Images
	CONTENT_TYPE_PNG  = "image/png"
	CONTENT_TYPE_JPEG = "image/jpeg"
	CONTENT_TYPE_GIF  = "image/gif"
	CONTENT_TYPE_WEBP = "image/webp"
	CONTENT_TYPE_SVG  = "image/svg+xml"

	// Archives
	CONTENT_TYPE_TAR_GZ = "application/gzip"
	CONTENT_TYPE_RAR    = "application/vnd.rar"
	CONTENT_TYPE_7Z     = "application/x-7z-compressed"

	//  BẮT BUỘC: Fallback an toàn cho file lạ hoặc download nhị phân
	CONTENT_TYPE_OCTET_STREAM = "application/octet-stream"

	//  NÊN CÓ: Để validate header request upload
	CONTENT_TYPE_FORM_DATA = "multipart/form-data"
)
