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

// Knob vận hành phase chunk của worker — service chỉ đọc, không định nghĩa.

// CHUNK_INTERVAL là nhịp worker quét bài chưa chunk — main lấy làm chu kỳ ticker.
const CHUNK_INTERVAL = 30 * time.Second

// CHUNK_CLAIM_LIMIT số bài hốt mỗi kỳ — claim là rẻ (1 transaction), làm
// không hết thì lease giữ bài lại, kỳ sau hốt tiếp.
const CHUNK_CLAIM_LIMIT = 6

// CHUNK_POOL_SIZE số bài chunk song song — mỗi bài giữ đồng thời: blob raw
// (trần 100MB) + text/XML giải nén (trần 80MB) + text rune khi split (text
// 20MB ≈ 80MB rune) + ảnh render/OCR, nên worst-case ~300MB/bài → pool 3
// ≈ 1GB nếu 3 file kịch trần cùng lúc. Container nhỏ thì giảm số này.
const CHUNK_POOL_SIZE = 3

// CHUNK_LEASE thời gian giữ claim — file 500 trang chunk trong vài phút là
// cùng; worker crash thì quá lease bài tự đủ điều kiện cho lần hốt sau.
const CHUNK_LEASE = 15 * time.Minute

// Knob vận hành phase embed của worker — service chỉ đọc, không định nghĩa.

// EMBED_INTERVAL là nhịp worker quét bài chưa embed — main lấy làm chu kỳ
// ticker. 10s để dev thấy kết quả nhanh; production tải cao thì nâng lên 1
// phút để đỡ quét DB trống.
const EMBED_INTERVAL = 10 * time.Second

// EMBED_CLAIM_LIMIT số bài hốt mỗi kỳ — ollama remote chậm, bài 500 trang
// (~1500 chunk ≈ 47 lần gọi) nuốt vài phút nên giữ ít.
const EMBED_CLAIM_LIMIT = 3

// EMBED_POOL_SIZE số bài embed song song — ollama remote là bottleneck nên
// chỉ 2, tránh dội request làm nó nghẽn rồi timeout hàng loạt.
const EMBED_POOL_SIZE = 2

// EMBED_LEASE thời gian giữ claim — đủ cho bài to nhất (500 trang) embed +
// upsert xong trong pool; crash thì quá lease tự reclaim.
const EMBED_LEASE = 30 * time.Minute

// EMBED_BATCH_SIZE số text mỗi lần gọi ollama — khớp giới hạn input 1 lần
// gọi của /api/embed, nhiều chunk thì chia nhiều đợt.
const EMBED_BATCH_SIZE = 32

// EMBED_CALL_ATTEMPTS số lần thử lại 1 lần gọi ollama/qdrant chập chờn, nghỉ
// backoff nhân đôi từ EMBED_CALL_BASE_DELAY.
const EMBED_CALL_ATTEMPTS = 3
const EMBED_CALL_BASE_DELAY = 2 * time.Second

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
