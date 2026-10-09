package services

import (
	"context"
	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/storage"
	"knowledge_ingestion/src/controller/services/internal/worker"
	"knowledge_ingestion/src/domain"
	"time"
)

// PurgeOptions nhịp janitor dọn chunk già — loader map từ config worker nên
// đổi config + restart là ăn ngay, không build lại. Test truyền tay struct này.
type PurgeOptions struct {
	// ChunkRetention tuổi tối đa chunk già (embedded sót + mồ côi) trước khi
	// dọn — row đang chờ retry không bao giờ bị đụng.
	ChunkRetention time.Duration
	// TrimInterval nhịp job quét dọn — rẻ (1 câu DELETE có index) nên hàng
	// ngày là đủ.
	TrimInterval time.Duration
}

// IArticlePurgeService janitor dọn bài đã xóa mềm — chỉ worker gọi, không
// qua HTTP nên trả (int, error) thuần thay vì dtos.Result (envelope đó dành
// cho handler Render). Mỗi kỳ quét: ListSoftDeleted → từng row xóa blob, xóa
// vector Qdrant, xóa chunk rows rồi xóa hẳn article row.
type IArticlePurgeService interface {
	PurgeDeleted(ctx context.Context) (int, error)
	// TrimOldChunks dọn chunk già (embedded quá hạn + mồ côi quá hạn), trả số
	// row đã xóa — janitor chạy hàng ngày, độc lập với purge bài xóa mềm.
	TrimOldChunks(ctx context.Context) (int64, error)
}

type articlePurgeService struct {
	articleRepo domain.ArticleJanitor
	chunkRepo   domain.IArticleChunkRepository
	vectors     domain.IVectorStore
	storage     storage.IStorage
	retention   time.Duration
}

func NewArticlePurgeService(articleRepo domain.ArticleJanitor, chunkRepo domain.IArticleChunkRepository, vectors domain.IVectorStore, storage storage.IStorage, opts PurgeOptions) IArticlePurgeService {
	return &articlePurgeService{articleRepo: articleRepo, chunkRepo: chunkRepo, vectors: vectors, storage: storage, retention: opts.ChunkRetention}
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
	traceID, err := worker.EnsureTraceID(ctx)
	if err != nil {
		return 0, err
	}
	// Detach tươi mỗi bài thay vì 1 ctx chung cả kỳ: xóa blob S3 + Qdrant của
	// 100 bài tốn hàng phút, ctx 30s tạo sớm chết giữa chừng khiến các bài
	// sau toàn rớt deadline oan.
	logs.Infow("purge: bắt đầu kỳ quét", "trace_id", traceID)
	articles, err := s.articleRepo.ListSoftDeleted(ctx, time.Now().Add(-constants.PURGE_GRACE_PERIOD), constants.PURGE_BATCH_SIZE)
	if err != nil {
		return 0, err
	}
	purged := 0
	for _, article := range articles {
		det := worker.Detach(ctx)
		func(dbCtx context.Context) {
			defer det.Close()
			if article.StorageKey != "" {
				if err := s.storage.Delete(dbCtx, article.StorageKey); err != nil {
					logs.Warnw("purge: xóa blob thất bại, để kỳ sau thử lại",
						"trace_id", traceID,
						"article_id", article.ID, "storage_key", article.StorageKey, "error", err)
					return
				}
			}
			// Thứ tự dọn sau blob: vector Qdrant → chunk rows → article row.
			// Qdrant delete idempotent (filter khớp 0 point vẫn success) nên
			// crash giữa chừng chạy lại chỉ ghi đè/bỏ qua, không mất dấu vết.
			// Chunk rows xóa trước article row để không còn chunk mồ côi trỏ tới
			// bài đã mất; bài nào lỗi ở bước nào cũng để kỳ sau thử lại từ đầu.
			if err := s.vectors.DeleteByArticle(dbCtx, article.ID); err != nil {
				logs.Warnw("purge: xóa vector thất bại, để kỳ sau thử lại",
					"trace_id", traceID,
					"article_id", article.ID, "error", err)
				return
			}
			if err := s.chunkRepo.DeleteByArticleID(dbCtx, article.ID); err != nil {
				logs.Warnw("purge: xóa chunk thất bại, để kỳ sau thử lại",
					"trace_id", traceID,
					"article_id", article.ID, "error", err)
				return
			}
			if err := s.articleRepo.HardDelete(dbCtx, article.ID); err != nil {
				logs.Warnw("purge: xóa hẳn row thất bại, để kỳ sau thử lại",
					"trace_id", traceID,
					"article_id", article.ID, "error", err)
				return
			}
			purged++
		}(det.Ctx)
	}
	logs.Infow("purge: xong 1 kỳ quét", "trace_id", traceID, "found", len(articles), "purged", purged)
	return purged, nil
}

// TrimOldChunks dọn chunk già quá retention: row đã embedded sót lại + row
// mồ côi. Row đang chờ retry không bị đụng nên chạy lúc nào cũng an toàn;
// DELETE có index nên kỳ không có gì dọn chỉ tốn 1 câu rẻ.
func (s *articlePurgeService) TrimOldChunks(ctx context.Context) (int64, error) {
	traceID, err := worker.EnsureTraceID(ctx)
	if err != nil {
		return 0, err
	}
	n, err := s.chunkRepo.DeleteStaleChunks(ctx, time.Now().Add(-s.retention))
	if err != nil {
		return 0, err
	}
	logs.Infow("purge: dọn chunk già xong", "trace_id", traceID, "trimmed", n)
	return n, nil
}
