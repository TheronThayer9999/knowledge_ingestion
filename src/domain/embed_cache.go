package domain

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"time"
)

// EmbedCacheEntry là 1 vector ĐÃ TÍNH, khoá theo (text_hash, model, dim).
//
// VÌ SAO CẦN: embed là bước đắt nhất của phase index — đo thật trên máy này
// bge-m3 chạy 7.77 chunk/s, còn mọi thao tác DB/Qdrant nhanh hơn hàng nghìn
// lần. Một bài 100MB là ~4.5 triệu chunk ≈ 7 ngày compute. Bất kỳ lần chạy
// lại nào (đổi chunker, rebuild vector, worker chết giữa chừng) mà gọi lại
// model trên những chunk đã tính là lãng phí không thể tha.
//
// Ý tưởng lấy từ AnythingLLM (`cachedVectorInformation`): cache kết quả embed
// theo định danh file để không embed lại. Khác ở chỗ bạn cache TRONG DB theo
// NỘI DUNG từng chunk thay vì theo cả file, nên:
//   - trùng lặp giữa các bài cũng ăn luôn (header "ĐIỀU 5: ..." lặp ở mọi
//     chunk của cùng điều, văn bản pháp lý lặp đoạn điều khoản rất nhiều);
//   - chỉ cần cache phần THIẾU, không phải toàn bộ file — job chết giữa
//     chừng vẫn tiếp tục được chứ không phải làm lại từ đầu.
//
// KHOÁ là hash của ĐÚNG chuỗi sẽ đưa vào model — tức đã kèm cả header mục
// do chunker.Annotate gắn. Hash thiếu header sẽ trả vector sai về mặt ngữ
// nghĩa (cùng nội dung nhưng context khác thì embedding khác).
type EmbedCacheEntry struct {
	// TextHash là sha256 hex của text đã embed.
	TextHash string `json:"text_hash" gorm:"primaryKey;size:64"`
	// Model là tên model đã dùng — cùng text mà đổi model thì vector khác,
	// nên model phải nằm trong khoá chứ không phải chỉ để tham khảo.
	Model string `json:"model" gorm:"primaryKey;size:128"`
	// Dim là số chiều vector — chống đọc nhầm khi đổi model mà quên đổi dim.
	Dim int `json:"dim" gorm:"primaryKey"`
	// Vector lưu dạng bytea (float32 little-endian) thay vì real[]: gorm đọc
	// ghi []byte an toàn hơn, và Postgres không cần extension.
	Vector []byte `json:"-" gorm:"type:bytea;not null"`
	// ChunkCount đếm số lần dùng — chỉ để dọn cache theo LRU sau này, không
	// ảnh hưởng kết quả.
	ChunkCount int       `json:"chunk_count" gorm:"not null;default:0"`
	CreatedAt  time.Time `json:"created_at"`
}

func (EmbedCacheEntry) TableName() string {
	return "embed_cache"
}

// EncodeVector đóng gói []float32 thành bytea little-endian. float32 đúng 4
// byte nên kích thước luôn chia hết cho 4 — dùng cho bất biến kiểm tra decode.
//
// Codec đặt CẠNH EmbedCacheEntry chứ không ở tầng repository: nó là thỏa
// thuận về CÁCH LƯU của cache, mà cả tầng service (khi ghi) lẫn tầng
// repository (khi đọc) đều phải dùng. Đặt trong repository sẽ buộc service
// import infrastructure — vi phạm luồng phụ thuộc của dự án.
func EncodeVector(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// DecodeVector là chiều ngược của EncodeVector. Trả lỗi nếu độ dài không
// chia hết cho 4 — vector hỏng thì embed lại còn hơn trả về mảng sai.
func DecodeVector(b []byte) ([]float32, error) {
	if len(b)%4 != 0 {
		return nil, ErrBadVector
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v, nil
}

// ErrBadVector báo dữ liệu cache hỏng (đổi cách encode, bytea bị cắt...) —
// caller nên coi như cache miss và embed lại, KHÔNG được fail job.
var ErrBadVector = errors.New("vector cache hỏng")

// EmbedCache truy cập cache embed — interface để phase index không import infra.
type EmbedCache interface {
	// Get trả map[text_hash]vector cho các hash trong danh sách, CHỈ những
	// hash thật sự có trong cache. Thiếu thì không có key — caller biết là
	// phải embed.
	//
	// Truyền cả model + dim để cache của model khác không lẫn sang: đổi
	// model embed là cache cũ vô nghĩa, phải coi như cache trống.
	Get(ctx context.Context, hashes []string, model string, dim int) (map[string][]float32, error)
	// PutMany ghi các vector mới. Phải idempotent: worker chết rồi chạy lại
	// sẽ insert lại đúng các hash đó, ON CONFLICT nên tăng counter thay vì
	// lỗi.
	PutMany(ctx context.Context, entries []*EmbedCacheEntry) error
	// Stats trả số entry + tổng dung lượng để admin nhìn được cache có đang
	// ăn không (không ăn thì hash key sai, hoặc text không lặp).
	Stats(ctx context.Context) (count int64, bytes int64, err error)
}