package constants

import "testing"

// TestQueueInvariants khoá các bất biến dễ vỡ khi chỉnh knob:
//
//   - claim không vượt pool: hốt dư là đốt lease của bài đang chờ slot;
//   - lease PHẢI lớn hơn job timeout: claim query reclaim row processing khi
//     next_retry_at <= now, nên lease ngắn hơn thời gian job chạy sẽ khiến
//     worker khác hốt lại bài đang chạy (xử lý trùng);
//   - SHUTDOWN_TIMEOUT phải lớn hơn SHUTDOWN_GRACE để còn thời gian drain
//     HTTP server sau khi worker dừng job.
func TestQueueInvariants(t *testing.T) {
	if EXTRACT_CLAIM_LIMIT > EXTRACT_POOL_SIZE {
		t.Fatalf("EXTRACT_CLAIM_LIMIT=%d > EXTRACT_POOL_SIZE=%d", EXTRACT_CLAIM_LIMIT, EXTRACT_POOL_SIZE)
	}
	if INDEX_CLAIM_LIMIT > INDEX_POOL_SIZE {
		t.Fatalf("INDEX_CLAIM_LIMIT=%d > INDEX_POOL_SIZE=%d", INDEX_CLAIM_LIMIT, INDEX_POOL_SIZE)
	}
	if EXTRACT_LEASE <= EXTRACT_JOB_TIMEOUT {
		t.Fatalf("EXTRACT_LEASE=%v phải lớn hơn EXTRACT_JOB_TIMEOUT=%v", EXTRACT_LEASE, EXTRACT_JOB_TIMEOUT)
	}
	if INDEX_LEASE <= INDEX_JOB_TIMEOUT {
		t.Fatalf("INDEX_LEASE=%v phải lớn hơn INDEX_JOB_TIMEOUT=%v", INDEX_LEASE, INDEX_JOB_TIMEOUT)
	}
	if SHUTDOWN_TIMEOUT <= SHUTDOWN_GRACE {
		t.Fatalf("SHUTDOWN_TIMEOUT=%v phải lớn hơn SHUTDOWN_GRACE=%v", SHUTDOWN_TIMEOUT, SHUTDOWN_GRACE)
	}
}
