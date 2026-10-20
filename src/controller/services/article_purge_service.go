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

// PurgeOptions nhịp janitor dọn trang trung gian sót — loader map từ config
// worker nên đổi config + restart là ăn ngay, không build lại. Test truyền tay
// struct này.
type PurgeOptions struct {
	// TrimInterval nhịp job quét dọn — rẻ nên hàng ngày là đủ.
	TrimInterval time.Duration
}

// IArticlePurgeService janitor dọn bài đã xóa mềm — chỉ worker gọi, không
// qua HTTP nên trả (int, error) thuần thay vì dtos.Result (envelope đó dành
// cho handler Render). Mỗi kỳ quét: ListSoftDeleted → từng row xóa blob, xóa
// vector Qdrant, xóa trang trung gian rồi xóa hẳn article row.
type IArticlePurgeService interface {
	PurgeDeleted(ctx context.Context) (int, error)
	// TrimOrphanPages dọn trang trung gian mồ côi — bài đã xóa hẳn mà job
	// extract chết trước lúc dọn. Trả số row đã xóa.
	TrimOrphanPages(ctx context.Context) (int64, error)
}

type articlePurgeService struct {
	articleRepo domain.ArticleJanitor
	pageRepo    domain.IArticlePageRepository
	vectors     domain.IVectorStore
	storage     storage.IStorage
}

func NewArticlePurgeService(articleRepo domain.ArticleJanitor, pageRepo domain.IArticlePageRepository, vectors domain.IVectorStore, storage storage.IStorage, opts PurgeOptions) IArticlePurgeService {
	return &articlePurgeService{articleRepo: articleRepo, pageRepo: pageRepo, vectors: vectors, storage: storage}
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
//   - Kỳ N: S3.Delete(K) success → HardDeleteMany lỗi (DB chết) → log + bỏ
//     qua. Row 5 còn nguyên trong DB (vẫn xóa mềm), blob đã mất.
//   - Kỳ N+1: ListSoftDeleted quét lại thấy row 5 (vẫn khớp deleted_at cũ)
//     → S3.Delete(K) lần 2 vẫn success vì S3 Delete idempotent (key không tồn
//     tại cũng báo thành công) → HardDeleteMany lúc DB đã sống → row bay.
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
//
// Chia 2 mốc rõ ràng để không lằng nhằng giữa "cô lập lỗi" và "ít round
// trip": S3 + Qdrant là hệ thống ngoài nên giữ từng bài một (mất mạng, mất
// binary, hỏng blob — 1 bài lỗi không được chặn bài khác); Postgres trong
// cùng LAN nên gom theo đợt.
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

	// Vòng này CHỈ làm phần hệ thống ngoài (blob S3, vector Qdrant) và giữ
	// cô lập lỗi từng bài: 1 blob hỏng không được chặn 99 bài còn lại. 2 lệnh
	// Postgres cuối kỳ (xóa chunk + xóa row) thì gom 1 câu mỗi lệnh — trước
	// đây 2 DELETE mỗi bài là 200 round trip cho 1 đợt 100 bài, trong khi
	// S3 + Qdrant vốn đã tốn hơn nhiều nên 2 câu kia không là gánh nặng.
	done := make([]int64, 0, len(articles))
	for _, article := range articles {
		// Job bị hủy (shutdown/hết giờ) thì dừng giữa đợt: S3/Qdrant đã xóa
		// idempotent nên kỳ sau quét lại đúng các row còn deleted_at là làm
		// tiếp, không cần cố chạy hết 100 bài sau khi ctx đã chết.
		if ctx.Err() != nil {
			logs.Infow("purge: job bị hủy, dừng phần còn lại cho kỳ sau",
				"trace_id", traceID, "remaining", len(articles)-len(done))
			break
		}
		det := worker.Detach(ctx)
		if ok := func(dbCtx context.Context) bool {
			defer det.Close()
			if article.StorageKey != "" {
				if err := s.storage.Delete(dbCtx, article.StorageKey); err != nil {
					logs.Warnw("purge: xóa blob thất bại, để kỳ sau thử lại",
						"trace_id", traceID,
						"article_id", article.ID, "storage_key", article.StorageKey, "error", err)
					return false
				}
			}
			// Qdrant delete idempotent (filter khớp 0 point vẫn success) nên
			// crash giữa chừng chạy lại chỉ ghi đè/bỏ qua, không mất dấu vết.
			if err := s.vectors.DeleteByArticle(dbCtx, article.ID); err != nil {
				logs.Warnw("purge: xóa vector thất bại, để kỳ sau thử lại",
					"trace_id", traceID,
					"article_id", article.ID, "error", err)
				return false
			}
			return true
		}(det.Ctx); ok {
			done = append(done, article.ID)
		}
	}
	if len(done) == 0 {
		logs.Infow("purge: xong 1 kỳ quét", "trace_id", traceID, "found", len(articles), "purged", 0)
		return 0, nil
	}

	// Chunk rows xóa TRƯỚC article row — không đảo, để không còn chunk mồ côi
	// trỏ tới bài đã mất. Hết sạch rồi mới xóa row: lỗi ở 1 trong 2 lệnh thì
	// cả đợt còn nguyên để kỳ sau quét lại (deleted_at cũ vẫn khớp, S3 delete
	// idempotent) — hội tụ chứ không nửa vời.
	det := worker.Detach(ctx)
	defer det.Close()
	if err := s.pageRepo.DeleteByArticles(det.Ctx, done); err != nil {
		logs.Error(err, "purge: xóa trang hàng loạt thất bại, để kỳ sau thử lại",
			"trace_id", traceID, "articles", len(done))
		return 0, err
	}
	if err := s.articleRepo.HardDeleteMany(det.Ctx, done); err != nil {
		logs.Error(err, "purge: xóa hàng loạt row thất bại, để kỳ sau thử lại",
			"trace_id", traceID, "articles", len(done))
		return 0, err
	}
	logs.Infow("purge: xong 1 kỳ quét", "trace_id", traceID, "found", len(articles), "purged", len(done))
	return len(done), nil
}

// TrimOrphanPages dọn trang trung gian mồ côi: trang còn nằm trong
// article_pages nhưng bài cha đã bị xoá hẳn (purge xoá row xong mới chết,
// hoặc job extract chết giữa lúc ghi). Bảng này chỉ là bàn giao nên xoá
// thoải mái, không cần tuổi quá hạn — chỉ cần bài cha không còn.
//
// Không chạm trang của bài còn sống kể cả bài đang extract dở: đó là
// checkpoint đang được dùng, xoá đi là mất tiến trình OCR.
func (s *articlePurgeService) TrimOrphanPages(ctx context.Context) (int64, error) {
	traceID, err := worker.EnsureTraceID(ctx)
	if err != nil {
		return 0, err
	}
	n, err := s.pageRepo.DeleteOrphan(ctx)
	if err != nil {
		return 0, err
	}
	logs.Infow("purge: dọn trang mồ côi xong", "trace_id", traceID, "deleted", n)
	return n, nil
}
