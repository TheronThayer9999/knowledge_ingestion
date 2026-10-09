package utils

import "time"

const (
	// MaxQueueAttempts số lần thử tối đa 1 bài trước khi đánh failed — file
	// lỗi thật (docx hỏng, ollama chết hẳn) không được retry vô hạn.
	MaxQueueAttempts = 10
	// QueueRetryBase cơ số backoff giữa các lần thử: 1ph, 2ph, 4ph...
	QueueRetryBase = time.Minute
	// MaxQueueDelay trần backoff — lỗi lâu thì thử lại mỗi 30 phút.
	MaxQueueDelay = 30 * time.Minute
)

// QueueBackoff tính giờ thử lại sau lần lỗi thứ attempts (đã tính lần vừa
// lỗi) — repo tự gọi trong Mark*Error nên service không tính tay. Nằm ở
// utils (không phải domain) vì đây là chính sách thời gian, không phải từ
// vựng persistence; domain chỉ giữ các const status pending/processing/....
func QueueBackoff(attempts int) time.Time {
	d := QueueRetryBase
	for i := 1; i < attempts && d < MaxQueueDelay; i++ {
		d *= 2
	}
	if d > MaxQueueDelay {
		d = MaxQueueDelay
	}
	return time.Now().Add(d)
}
