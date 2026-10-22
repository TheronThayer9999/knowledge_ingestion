package services

import (
	"errors"
	"testing"

	"knowledge_ingestion/src/domain"
)

// TestEncodeDecodeVectorRoundTrip kiểm tra codec vector không mất dữ liệu.
// EncodeVector/DecodeVector vẫn được dùng cho embed (mặc dù cache đã bỏ,
// codec đặt cạnh EmbedCacheEntry trong domain và vẫn là thỏa thuận lưu trữ).
func TestEncodeDecodeVectorRoundTrip(t *testing.T) {
	in := []float32{0.5, -1.25, 3e-7}
	out, err := domain.DecodeVector(domain.EncodeVector(in))
	if err != nil {
		t.Fatalf("decode lỗi: %v", err)
	}
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("[%d] = %v, want %v", i, out[i], in[i])
		}
	}
	if _, err := domain.DecodeVector([]byte{1, 2, 3}); !errors.Is(err, domain.ErrBadVector) {
		t.Errorf("độ dài lẻ phải trả ErrBadVector, got %v", err)
	}
}