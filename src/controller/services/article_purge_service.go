package services

import (
	"context"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/storage"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/domain"
	"time"
)

const (
	// PurgeGracePeriod chỉ dọn bài đã xóa mềm quá 5 phút — cửa sổ undo cho
	// user đổi ý + tránh đua với request đọc ngay sau khi xóa. Export để
	// worker lấy làm chu kỳ ticker: 1 nguồn sự thật duy nhất, khỏi 2 hằng
	// 5-phút ở 2 file lệch nhau lúc nào không hay.
	PurgeGracePeriod = 5 * time.Minute
	// purgeBatchSize số row xử lý mỗi kỳ quét — đủ nhỏ để 1 kỳ chạy xong
	// nhanh, còn tồn thì kỳ sau dọn tiếp.
	purgeBatchSize = 100
)

// IArticlePurgeService janitor dọn bài đã xóa mềm — chỉ worker gọi, không
// qua HTTP nên trả (int, error) thuần thay vì dtos.Result (envelope đó dành
// cho handler Render). Mỗi kỳ quét: ListSoftDeleted → từng row xóa blob rồi
// xóa hẳn.
type IArticlePurgeService interface {
	PurgeDeleted(ctx context.Context) (int, error)
}

type articlePurgeService struct {
	articleRepo domain.IArticleRepository
	storage     storage.IStorage
}

func NewArticlePurgeService(articleRepo domain.IArticleRepository, storage storage.IStorage) IArticlePurgeService {
	return &articlePurgeService{articleRepo: articleRepo, storage: storage}
}

// PurgeDeleted dọn 1 đợt bài quá grace period, trả số row đã xóa hẳn.
// Thứ tự cố định blob-trước-row-sau, thay cho transaction xuyên hệ thống
// (S3 không có transaction nên không thể gộp 2 bước vào 1 commit).
//
// Vì sao KHÔNG được làm ngược lại (xóa row trước, S3 lỗi thì "rollback"
// bằng cách insert lại): rollback tay chỉ cứu được khi app còn sống. Nếu
// crash đúng cửa sổ sau khi DB commit mà trước khi S3 xóa xong (mất điện,
// OOM, deploy), row đã mất hẳn, blob còn đó nhưng không còn reference nào
// trong DB để kỳ sau tìm lại — mồ côi VĨNH VIỄN, retry cũng không cứu được.
//
// Thứ tự blob-trước thì mọi điểm lỗi đều hội tụ khi chạy lại, ví dụ DB chết
// đúng giữa chừng:
//   - Kỳ N: S3.Delete(K) success → HardDelete(5) lỗi (DB chết) → log + bỏ
//     qua. Row 5 còn nguyên trong DB (vẫn xóa mềm), blob đã mất.
//   - Kỳ N+1: ListSoftDeleted quét lại thấy row 5 (vẫn khớp deleted_at cũ)
//     → S3.Delete(K) lần 2 vẫn success vì S3 Delete idempotent (key không tồn
//     tại cũng báo thành công) → HardDelete(5) lúc DB đã sống → row bay.
//
// Nguyên tắc để nhớ: row DB là con trỏ duy nhất tới blob — không bao giờ hủy
// con trỏ trước khi thứ nó trỏ tới đã biến mất.
//
// Row nào lỗi ở bước nào cũng chỉ log + bỏ qua, kỳ sau thử lại.
//
// Mỗi lần gọi lấy trace_id runner đã gắn vào ctx (UUIDv7, sort được theo
// thời gian) gắn vào mọi dòng log của kỳ đó — grep 1 id là thấy trọn vòng
// đời 1 kỳ quét (bắt đầu → từng row lỗi → xong), kể cả khi 2 container cùng
// chạy. Gọi trực tiếp không qua runner (test, tool tay) thì ctx trống, lúc
// đó tự sinh fallback để log vẫn lần được.
func (s *articlePurgeService) PurgeDeleted(ctx context.Context) (int, error) {
	traceID := utils.TraceIDFromCtx(ctx)
	if traceID == "" {
		var err error
		if traceID, err = utils.NewUUIDv7(); err != nil {
			return 0, err
		}
	}
	logs.Infow("purge: bắt đầu kỳ quét", "trace_id", traceID)
	articles, err := s.articleRepo.ListSoftDeleted(ctx, time.Now().Add(-PurgeGracePeriod), purgeBatchSize)
	if err != nil {
		return 0, err
	}
	purged := 0
	for _, article := range articles {
		if article.StorageKey != "" {
			if err := s.storage.Delete(ctx, article.StorageKey); err != nil {
				logs.Warnw("purge: xóa blob thất bại, để kỳ sau thử lại",
					"trace_id", traceID,
					"article_id", article.ID, "storage_key", article.StorageKey, "error", err)
				continue
			}
		}
		if err := s.articleRepo.HardDelete(ctx, article.ID); err != nil {
			logs.Warnw("purge: xóa hẳn row thất bại, để kỳ sau thử lại",
				"trace_id", traceID,
				"article_id", article.ID, "error", err)
			continue
		}
		purged++
	}
	logs.Infow("purge: xong 1 kỳ quét", "trace_id", traceID, "found", len(articles), "purged", purged)
	return purged, nil
}
