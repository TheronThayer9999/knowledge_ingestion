package constants

import "time"

// Giới hạn upload file tập trung ở đây để tường minh — mọi tầng dùng chung,
// đổi một chỗ là đủ. Đặt tên UPPER_CASE theo quy ước const dùng chung.

// PRESIGN_EXPIRY là thời hạn sống của URL upload — hết hạn client phải xin lại.
const PRESIGN_EXPIRY = 15 * time.Minute

// PRESIGN_DOWNLOAD_EXPIRY là thời hạn sống của URL tải file — ngắn hơn upload
// vì chữ ký chỉ cần sống đủ cho một cú click: browser gửi request ngay khi nhận
// response. Chữ ký càng ngắn thì nếu có lọt vào log hay lịch sử browser cũng
// chóng hết dùng được.
const PRESIGN_DOWNLOAD_EXPIRY = 5 * time.Minute

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

// CHAT_ATTACHMENT_MAX_BYTES chốt dung lượng tệp đính kèm chat — 15MB. Khác
// 500MB của kho tri thức: tệp chat được trích ĐỒNG BỘ ngay trong request nên
// phải giữ thời gian/RAM trong một nhịp HTTP, không xếp hàng worker.
const CHAT_ATTACHMENT_MAX_BYTES = 15 << 20

// CHAT_ATTACHMENT_MAX_CHARS trần ký tự của tệp đính kèm đưa vào prompt. Cắt
// theo rune để không xé ký tự Việt; phần dư bị bỏ chứ không làm hỏng lượt hỏi
// (context model có hạn, tệp 500 trang không nhét vừa).
const CHAT_ATTACHMENT_MAX_CHARS = 60000

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

// EXTRACT_POOL_SIZE số bài trích song song. Để 1: gopdf nạp cả PDF vào RAM
// (đo: PDF 90MB đỉnh ~920MB), 3 job song song là ~3GB chỉ riêng extract.
// Trích tuần tự thì nhanh đủ, vì bottleneck thật là index.
const EXTRACT_POOL_SIZE = 1

// EXTRACT_CLAIM_LIMIT số bài hốt mỗi kỳ — BẰNG pool size. Claim là đánh dấu
// processing + đặt lease, nên hốt dư nghĩa là bài chưa chạy đã bị đốt lease:
// chờ lâu quá lease thì worker khác claim lại và xử lý trùng. Hốt đúng số
// slot chạy được thì không bài nào ngồi chờ vô ích.
const EXTRACT_CLAIM_LIMIT = EXTRACT_POOL_SIZE

// EXTRACT_PAGE_WORKERS số khoảng trang của MỘT PDF trích song song (mỗi khoảng
// là 1 tiến trình pdftotext, trang scan thêm render + OCR). Tổng tiến trình
// poppler/OCR đồng thời = EXTRACT_PAGE_WORKERS, vì EXTRACT_POOL_SIZE = 1.
const EXTRACT_PAGE_WORKERS = 4

// EXTRACT_LEASE thời gian giữ claim — PHẢI LỚN HƠN EXTRACT_JOB_TIMEOUT.
// Claim query reclaim cả row processing khi next_retry_at <= now, nên lease
// ngắn hơn thời gian job chạy = worker khác hốt lại bài đang chạy (xử lý
// trùng). 60 phút > trần 45 phút; crash thì chờ hết lease mới reclaim.
const EXTRACT_LEASE = 60 * time.Minute

// EXTRACT_FLUSH_PAGES số trang ghi 1 lô ra artifact DỞ. Job chết giữa file
// 500 trang thì kỳ sau nhảy tiếp từ trang kế tiếp thay vì OCR lại từ đầu.
// Đổi số này là đánh đổi giữa an toàn và round trip — 20 trang ≈ 40 giây OCR
// bị làm lại, đợt 500 trang chỉ 25 lần PUT.
const EXTRACT_FLUSH_PAGES = 20

// EXTRACT_JOB_TIMEOUT trần thời gian 1 kỳ quét extract — PHẢI NHỎ HƠN
// EXTRACT_LEASE (xem comment lease).
const EXTRACT_JOB_TIMEOUT = 45 * time.Minute

// Knob vận hành phase index của worker — service chỉ đọc, không định nghĩa.
//
// Index là phase CUỐI: cắt chunk từ toàn văn bản đã trích rồi embed + upsert
// Qdrant, làm chung một lượt (xem IArticleIndexService để biết vì sao gộp).

// INDEX_INTERVAL là nhịp worker quét bài chưa index — main lấy làm chu kỳ
// ticker. 10s để dev thấy kết quả nhanh; production tải cao thì nâng lên 1
// phút để đỡ quét DB trống.
const INDEX_INTERVAL = 10 * time.Second

// INDEX_POOL_SIZE số bài index song song — ollama remote là bottleneck nên
// chỉ 2, tránh dội request làm nó nghẽn rồi timeout hàng loạt.
const INDEX_POOL_SIZE = 2

// INDEX_CLAIM_LIMIT số bài hốt mỗi kỳ — BẰNG pool size, cùng lý do với
// EXTRACT_CLAIM_LIMIT: hốt dư là đốt lease của bài còn đang chờ slot.
const INDEX_CLAIM_LIMIT = INDEX_POOL_SIZE

// INDEX_LEASE thời gian giữ claim — PHẢI LỚN HƠN INDEX_JOB_TIMEOUT, cùng lý
// do với EXTRACT_LEASE: lease ngắn hơn job khiến worker khác reclaim bài đang
// chạy. Crash thì quá lease tự reclaim.
const INDEX_LEASE = 60 * time.Minute

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

// INDEX_JOB_TIMEOUT trần thời gian 1 kỳ quét index — PHẢI NHỎ HƠN INDEX_LEASE
// (xem comment lease). Timeout ở đây chỉ là chốt an toàn cuối: nhờ checkpoint
// ghi sau mỗi INDEX_BATCH_SIZE nên hết giờ không còn là vòng lặp vô hạn, kỳ
// sau chỉ làm nốt phần còn lại.
const INDEX_JOB_TIMEOUT = 45 * time.Minute

// Knob vận hành bộ tự chữa mất vector (reconcile) — so articles.index_done_chunks
// với point thật trên Qdrant, bài "done" mà kho trống thì đưa về hàng đợi index.
// Cần vì ensureCollection drop/recreate collection khi shape vector đổi: DB vẫn
// báo done trong khi search trả kết quả thiếu.

// RECONCILE_PAGE_SIZE số bài done nạp mỗi lượt quét (LIMIT của
// ListByIndexStatus) — quét theo lô, không kéo cả bảng.
const RECONCILE_PAGE_SIZE = 200

// RECONCILE_MAX_ARTICLES trần số bài quét trong MỘT lượt reconcile: mỗi bài là
// 1 câu Count gửi lên Qdrant, kho có triệu bài thì không được phép biến lượt
// quét thành hàng nghìn round trip. Quét chưa hết thì log báo "thừa" để người
// vận hành biết, job vẫn hội tụ dần qua nhiều kỳ.
const RECONCILE_MAX_ARTICLES = 2000

// RECONCILE_INTERVAL nhịp quét — thưa hơn index nhiều vì đây là lưới chắn
// cho tai biến (mất collection), không phải đường dữ liệu hằng ngày; mất
// collection thì guard trong phase 1 đã chặn bài mới thành ma ngay.
const RECONCILE_INTERVAL = 6 * time.Hour

// RECONCILE_TIMEOUT trần 1 lượt quét — phải nhỏ hơn RECONCILE_INTERVAL để kỳ
// sau không chồng lên kỳ đang chạy.
const RECONCILE_TIMEOUT = 5 * time.Minute

// GHOST_SAMPLE_SIZE số id ma đưa vào response consistency — đủ để kiểm chứng
// bằng tay mà không đẩy payload phình theo số bài hỏng.
const GHOST_SAMPLE_SIZE = 20

// Knob vận hành phase compile của worker (P2.4 phase 1b) — đọc mục từ Qdrant,
// LLM sinh summary từng mục, ghi article_sections. Chạy SAU index, bật mặc
// định: bài index done tự vào hàng đợi.

// COMPILE_INTERVAL nhịp worker quét bài chờ compile — LLM call mắc hơn embed
// nên nhịp thưa hơn index để đỡ quét DB trống.
const COMPILE_INTERVAL = 30 * time.Second

// COMPILE_POOL_SIZE số bài compile song song — bottleneck là LLM provider
// (retry+rate limit đã nằm trong transport), 2 là đủ ấm.
const COMPILE_POOL_SIZE = 2

// COMPILE_CLAIM_LIMIT số bài hốt mỗi kỳ — BẰNG pool size, cùng lý do với
// INDEX_CLAIM_LIMIT.
const COMPILE_CLAIM_LIMIT = COMPILE_POOL_SIZE

// COMPILE_LEASE/COMPILE_JOB_TIMEOUT cùng luật extract/index: timeout PHẢI
// NHỎ HƠN lease để worker khác không reclaim bài đang chạy.
//
// Timeout cả bài nâng lên 60 phút (trước 25) vì bài lớn có hàng nghìn mục;
// nhưng KHÔNG phải chỗ dựa vào để không mất tiến trình — checkpoint
// compile_done_sections đảm nhiệm phần đó (xem compile service).
const COMPILE_LEASE = 90 * time.Minute
const COMPILE_JOB_TIMEOUT = 60 * time.Minute

// COMPILE_CALL_TIMEOUT timeout của MỘT lượt gọi LLM tóm tắt — tách riêng khỏi
// timeout cả bài. Lý do: 1 lượt model kẹt (provider treo) chỉ nên làm hỏng
// lô mục đang chạy, không kéo cả bài (hàng nghìn mục) bị giết kèm. Lớn hơn
// timeout HTTP client (120s) để còn chỗ cho lượt thử lại ở tầng transport.
const COMPILE_CALL_TIMEOUT = 5 * time.Minute

// COMPILE_SECTION_BATCH số mục gộp trong 1 lượt gọi LLM. Trước đây 1 mục/lượt
// nên bài 2000 mục tốn 2000 request provider; gộp 8 mục/lượt giảm ~8 lần số
// request. 8 là điểm hoà đội: prompt vừa đủ context của model free mà vẫn
// còn 8 mục/lượt để giảm chi phí mạnh.
const COMPILE_SECTION_BATCH = 8

// COMPILE_MAX_CHUNKS_PER_SECTION trần chunk nạp cho 1 mục khi gọi LLM — mục
// dài (cả chương) chỉ cần vài chunk đầu là tóm đúng ý, chặn prompt phình
// quá cửa sổ context của model free.
const COMPILE_MAX_CHUNKS_PER_SECTION = 6

// COMPILE_CACHE_TTL cache Redis summary theo hash (model|tên mục|nội dung)
// — tái build mục không đổi nội dung thì không tốn LLM lần hai; 30 ngày đủ
// phủ vòng đời tài liệu sửa chậm.
const COMPILE_CACHE_TTL = 30 * 24 * time.Hour

// WORKER_HEARTBEAT_INTERVAL nhịp worker tự ghi nhịp sống (bảng
// worker_heartbeats) — API/UI dựa vào đó để phân biệt "worker đang xử lý"
// với "worker đã tắt". Không có nhịp này thì bài kẹt chờ trông y hệt bài
// đang chạy và user ngồi đợi vô vọng.
const WORKER_HEARTBEAT_INTERVAL = 15 * time.Second

// WORKER_OFFLINE_AFTER quá ngưỡng này mà không thấy heartbeat thì coi như
// worker tắt — rộng hơn 3 nhịp để 1 lần ghi DB chậm không báo động nhầm.
const WORKER_OFFLINE_AFTER = 3 * WORKER_HEARTBEAT_INTERVAL

// SHUTDOWN_GRACE thời gian worker chờ job đang chạy thoát sau khi cancel —
// đủ để service thấy ctx.Done giữa 2 batch/page và ghi nốt trạng thái bằng
// detached ctx. Quá hạn thì vẫn đóng DB/Qdrant để không treo process.
const SHUTDOWN_GRACE = 30 * time.Second

// SHUTDOWN_TIMEOUT trần thời gian cho toàn bộ app.Stop (fx chạy các hook
// ngược thứ tự: worker chờ job tối đa SHUTDOWN_GRACE, rồi HTTP server
// Shutdown nốt request đang chạy). Phải lớn hơn SHUTDOWN_GRACE để server còn
// thời gian drain; không có trần thì 1 request treo giữ process mãi.
const SHUTDOWN_TIMEOUT = 60 * time.Second

// Knob vận hành phase purge của worker — service chỉ đọc, không định nghĩa.

// PURGE_GRACE_PERIOD chỉ dọn bài đã xóa mềm quá 5 phút — cửa sổ undo cho user
// đổi ý + tránh đua với request đọc ngay sau khi xóa. Main lấy làm chu kỳ
// ticker: 1 nguồn sự thật duy nhất, khỏi 2 hằng 5-phút ở 2 file lệch nhau.
const PURGE_GRACE_PERIOD = 5 * time.Minute

// PURGE_BATCH_SIZE số row xử lý mỗi kỳ quét — đủ nhỏ để 1 kỳ chạy xong nhanh,
// còn tồn thì kỳ sau dọn tiếp.
const PURGE_BATCH_SIZE = 100

// REFRESH_TOKEN_JANITOR_INTERVAL nhịp dọn refresh token chết — 1 ngày 1 lần
// là đủ, bảng chỉ phình theo số lần đăng nhập chứ không theo lưu lượng đọc.
const REFRESH_TOKEN_JANITOR_INTERVAL = 24 * time.Hour

// REFRESH_TOKEN_RETENTION giữ token đã hết hạn/bị thu hồi thêm 7 ngày để còn
// đối soát khi điều tra reuse detection, sau đó xoá cứng.
const REFRESH_TOKEN_RETENTION = 7 * 24 * time.Hour

// QDRANT_COLLECTION là collection duy nhất worker dùng — 1 collection cho mọi
// article, phân biệt bằng payload article_id (filter + xóa theo bài).
const QDRANT_COLLECTION = "article_chunks"

// SEARCH_DEFAULT_LIMIT số hit search mặc định — agent RAG chỉ cần vài đoạn
// gần nhất làm ngữ cảnh, nhiều hơn thì xin rõ limit.
const SEARCH_DEFAULT_LIMIT = 5

// SEARCH_MAX_LIMIT trần hit mỗi query search — chặn request vô hạn làm nặng
// Qdrant + response phình.
const SEARCH_MAX_LIMIT = 20

// SEARCH_TEXT_FETCH số chunk nửa keyword nạp về trước khi xếp hạng từ.
// Scroll của Qdrant KHÔNG có thứ tự liên quan — nó trả N point khớp từ khóa
// theo thứ tự nội bộ, nên lấy đúng limit thì ranking của RRF dựa trên một mẫu
// ngẫu nhiên. Gọi dư rồi tự xếp hạng theo độ phủ từ (retrieval.RankLexical)
// và cắt: mẫu càng rộng thì chunk thật sự khớp đứng vào top.
const SEARCH_TEXT_FETCH = 200

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
const (
	CATEGORY_TYPE_PRODUCT = "product"
	CATEGORY_TYPE_ARTICLE = "article"
)

// Knob của hàng đợi quét (tab Quét): src/common/scanocr đo + OCR, ảnh trang
// render cho tab Xem & sửa, ocr_jobs/ocr_job_pages giữ trạng thái. Tách
// hẳn khỏi knob extract/index/compile vì đây là luồng TẠM theo người dùng, có
// TTL riêng — xem plans/2026-10-30-dot-31.

// SCAN_OBJECT_PREFIX "thư mục ảo" của bản quét tạm trong bucket. Tách khỏi
// uploads/ và extracted/ để janitor, ACL và dung lượng nhìn thành một nhóm
// riêng — xoá nhầm luồng kho là mất bài, xoá nhầm luồng quét chỉ mất bản nháp.
const SCAN_OBJECT_PREFIX = "scan/"

// SCAN_PAGE_INSERT_BATCH số row trang mỗi câu INSERT. Row lúc tạo rất nhẹ (words
// chỉ được ghi sau OCR) nên lô lớn vẫn dưới trần 65535 param của Postgres; file
// 300 trang là 2 câu INSERT thay vì 300.
const SCAN_PAGE_INSERT_BATCH = 200

// SCAN_CLAIM_PAGES số trang một lô claim, ĐỒNG THỜI là mặc định khi caller
// truyền limit <= 0 — repo không được phép để Limit(0) biến câu claim thành
// "khoá cả bảng".
const SCAN_CLAIM_PAGES = 4

// SCAN_PAGE_LEASE thời hạn một trang bị giữ. Trần thời gian OCR một trang
// (scan_ocr.page_timeout_sec, mặc định 180s) phải NHỎ hơn lease, nếu không worker
// khác reclaim đúng lúc trang gần xong và OCR trùng twice.
const SCAN_PAGE_LEASE = 5 * time.Minute

// SCAN_PAGE_BACKOFF chờ giữa hai lần thử cùng một trang — OCR lỗi phần lớn do
// trang đang bị kẹt tài nguyên (disk, engine treo), nghỉ 30s là đủ khỏi.
const SCAN_PAGE_BACKOFF = 30 * time.Second

// SCAN_JOB_LEASE hẹn lease cấp job (chỉ để lịch sử không hiện job "chờ" vô
// hạn); thiết kế claim theo trang nên job không tự chiếm chỗ ai.
const SCAN_JOB_LEASE = 30 * time.Minute

// SCAN_PAGE_WINDOW số trang nạp một lượt khi đọc bảng trang theo cửa sổ (dọn
// object khi xoá job, janitor) — cùng ý nghĩa với cửa sổ trượt của phase index:
// RAM không phụ thuộc số trang của tệp.
const SCAN_PAGE_WINDOW = 20

// SCAN_PREVIEW_JPEG_QUALITY chất lượng JPEG của ẢNH XEM trang (tab Xem & sửa).
// Ảnh xem chỉ để đối chiếu chữ OCR nên cắt byte mạnh: một trang A4 150DPI còn
// khoảng 1/4 dung lượng PNG mà mắt thường không phân biệt trên bản scan.
const SCAN_PREVIEW_JPEG_QUALITY = 80

// SCAN_RENDER_CONCURRENCY số lượt render ảnh trang chạy ĐỒNG THỜI trong process
// API. Mỗi lượt là một tiến trình pdftoppm; nhiều người mở tab Xem & sửa cùng
// lúc mà không chặn thì số tiến trình (và RAM) tỉ lệ với số request. Chờ quá
// lâu thì trả ảnh rỗng (best-effort), không để request treo vô hạn.
const SCAN_RENDER_CONCURRENCY = 2

// SCAN_LIST_LIMIT_DEFAULT/MAX phân trang tab Lịch sử. Trần ở repo để một cú
// limit=1000000 từ client không biến thành một cú seq scan cả bảng.
const SCAN_LIST_LIMIT_DEFAULT = 20
const SCAN_LIST_LIMIT_MAX = 100

// SCAN_JANITOR_BATCH số job dọn mỗi lượt; INTERVAL thưa vì TTL tính bằng giờ.
const SCAN_JANITOR_BATCH = 200
const SCAN_JANITOR_INTERVAL = 1 * time.Hour
const SCAN_JANITOR_TIMEOUT = 10 * time.Minute

// SCAN_TMP_TTL / SCAN_PUSHED_TTL là hạn mặc định của bản quét tạm khi config
// không nói gì (scan_ocr.temp_ttl_hours / pushed_ttl_hours thắng). Đã push vào
// kho thì bản tạm không còn là nguồn duy nhất nên giữ lâu hơn cho người dùng
// quay lại tải PDF, nhưng vẫn phải hết hạn — tài liệu scan nhạy cảm không được
// nằm trong kho vĩnh viễn chỉ vì quên bấm xoá.
const SCAN_TMP_TTL = 24 * time.Hour
const SCAN_PUSHED_TTL = 7 * 24 * time.Hour
