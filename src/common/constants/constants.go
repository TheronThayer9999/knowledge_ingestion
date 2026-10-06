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
