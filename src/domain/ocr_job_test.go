package domain

import "testing"

// TestOCRJobStatusFromProgress khoá máy trạng thái job: chỉ MỘT hàm này suy ra
// status từ đếm trang, nên mọi trường hợp dưới đây là hành vi chốt của cả hàng
// đợi — sai một là Lịch sử hiện sai trạng thái mà không ai hiểu vì sao.
func TestOCRJobStatusFromProgress(t *testing.T) {
	cases := []struct {
		name string
		p    ScanJobProgress
		want string
	}{
		{"còn trang chờ thì đang chạy", ScanJobProgress{Total: 5, Done: 2, Pending: 3}, QueueProcessing},
		{"có trang đang chạy thì đang chạy", ScanJobProgress{Total: 5, Done: 4, Running: 1}, QueueProcessing},
		{"hết lượt nhưng toàn hỏng thì failed", ScanJobProgress{Total: 3, Failed: 3}, QueueFailed},
		{"hỗn hợp done + failed vẫn done (PDF còn xem được)", ScanJobProgress{Total: 4, Done: 3, Failed: 1}, QueueDone},
		{"trang bỏ qua vì đã có chữ tính là done", ScanJobProgress{Total: 2, Done: 2}, QueueDone},
		// Job 0 trang không tồn tại thật (service chặn từ upload), nhưng nếu
		// đếm ra rỗng thì trả done chứ không failed: failed sẽ nói dối rằng có
		// lỗi trong khi chưa từng có việc gì để làm.
		{"không trang nào", ScanJobProgress{}, QueueDone},
	}
	for _, tc := range cases {
		if got := OCRJobStatusFromProgress(tc.p); got != tc.want {
			t.Errorf("%s: OCRJobStatusFromProgress(%+v) = %q, muốn %q", tc.name, tc.p, got, tc.want)
		}
	}
}
