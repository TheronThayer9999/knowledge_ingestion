package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"knowledge_ingestion/src/common/chunker"
	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/extractor"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/storage"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/controller/services/internal/worker"
	"knowledge_ingestion/src/domain"
)

// IArticleChunkService worker phase 1: cắt text file Word thành chunk lưu DB
// — chỉ worker gọi, không qua HTTP nên trả (int, error) thuần thay vì
// dtos.Result (envelope đó dành cho handler Render). Phase 2 (embed + Qdrant)
// đọc row chunk này, không đụng tới file nữa.
type IArticleChunkService interface {
	ChunkPending(ctx context.Context) (int, error)
}

type articleChunkService struct {
	articleRepo domain.ArticleChunkQueue
	chunkRepo   domain.IArticleChunkRepository
	storage     storage.IStorage
	// ocr cấu hình Tesseract + renderer — build từ config ở loader, truyền
	// xuống ExtractPages. Struct value (không phải interface) vì chỉ là tham
	// số chạy, không cần mock ở service (test truyền OCRConfig{} là tắt).
	ocr extractor.OCRConfig
	// splitter chiến lược cắt chunk (Eino recursive/semantic hoặc manual) —
	// loader chọn theo config worker.chunker, dựng 1 lần rồi tái dùng.
	splitter chunker.Chunker
}

func NewArticleChunkService(articleRepo domain.ArticleChunkQueue, chunkRepo domain.IArticleChunkRepository, storage storage.IStorage, ocr extractor.OCRConfig, splitter chunker.Chunker) IArticleChunkService {
	return &articleChunkService{articleRepo: articleRepo, chunkRepo: chunkRepo, storage: storage, ocr: ocr, splitter: splitter}
}

// ChunkPending claim 1 đợt bài rồi chunk song song trong pool giới hạn, trả
// số bài đã cắt xong. Bài nào lỗi cũng chỉ log + MarkChunkError (backoff,
// quá số lần thì failed), không chặn bài khác. Claim bằng SKIP LOCKED nên N
// worker cùng chạy không giẫm nhau.
//
// Mỗi lần gọi lấy trace_id runner đã gắn vào ctx gắn vào mọi dòng log — cùng
// quy ước với PurgeDeleted, gọi trực tiếp (test, tool tay) thì tự sinh fallback.
func (s *articleChunkService) ChunkPending(ctx context.Context) (int, error) {
	traceID, err := worker.EnsureTraceID(ctx)
	if err != nil {
		return 0, err
	}
	logs.Infow("chunk: bắt đầu kỳ quét", "trace_id", traceID)
	articles, err := s.articleRepo.ClaimChunkPending(ctx, constants.CHUNK_CLAIM_LIMIT, constants.CHUNK_LEASE)
	if err != nil {
		return 0, err
	}
	var chunked atomic.Int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, constants.CHUNK_POOL_SIZE)
	for _, article := range articles {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(a *domain.Article) {
			defer wg.Done()
			defer func() { <-sem }()
			s.chunkClaimed(ctx, traceID, a, &chunked)
		}(article)
	}
	wg.Wait()
	n := int(chunked.Load())
	logs.Infow("chunk: xong 1 kỳ quét", "trace_id", traceID, "found", len(articles), "chunked", n)
	return n, nil
}

// chunkClaimed xử lý 1 bài đã claim: lỗi thì MarkChunkError (attempts = số
// lỗi trước + 1 lần này; lỗi xác định như file hỏng thì failed luôn), xong thì
// MarkChunkDone. Đánh dấu bằng detached ctx để ghi được dù job hết timeout;
// job hết timeout (ctx hủy) thì bỏ qua không đánh lỗi oan.
func (s *articleChunkService) chunkClaimed(ctx context.Context, traceID string, article *domain.Article, chunked *atomic.Int64) {
	// KHÔNG tạo dbCtx dùng chung ở đây: chunkOne tải blob + OCR tốn hàng
	// phút, ctx 30s tạo sớm chết trước khi tới lượt ghi done/error.
	if err := s.chunkOne(ctx, traceID, article); err != nil {
		if ctx.Err() != nil {
			logs.Infow("chunk: job hết timeout, bỏ qua không đánh lỗi",
				"trace_id", traceID, "article_id", article.ID)
			return
		}
		attempts := article.ChunkAttempts + 1
		if extractor.IsPermanent(err) {
			// Blob immutable nên lỗi xác định (loại chưa hỗ trợ, file hỏng,
			// ảnh trắng) thử lại cũng vậy — failed luôn thay vì retry 10 lần.
			attempts = utils.MaxQueueAttempts
		}
		logs.Warnw("chunk: bỏ qua bài, thử lại theo backoff",
			"trace_id", traceID,
			"article_id", article.ID, "storage_key", article.StorageKey, "error", err)
		det := worker.Detach(ctx)
		defer det.Close()
		if merr := s.articleRepo.MarkChunkError(det.Ctx, article.ID, attempts); merr != nil {
			logs.Error(merr, "chunk: không đánh dấu lỗi được", "trace_id", traceID, "article_id", article.ID)
		}
		return
	}
	det := worker.Detach(ctx)
	defer det.Close()
	if err := s.articleRepo.MarkChunkDone(det.Ctx, article.ID); err != nil {
		logs.Error(err, "chunk: không đánh dấu done được", "trace_id", traceID, "article_id", article.ID)
		return
	}
	chunked.Add(1)
}

// chunkOne cắt 1 bài theo từng Page: tải blob → trích theo trang → split từng
// trang → lưu batch. Trang trắng (scan toàn ảnh, trang lỗi) thì bỏ qua; cả
// file không còn trang nào có chữ thì coi như lỗi để không đánh dấu "xong"
// giả — MarkChunkError lùi giờ thử thay vì mất bài vĩnh viễn. ChunkIndex đếm
// liên tục xuyên trang để giữ thứ tự đọc, PageNum nhớ trang gốc. Bài đã có
// chunk (claim trùng do lease race) thì bỏ qua luôn, service đánh done.
func (s *articleChunkService) chunkOne(ctx context.Context, traceID string, article *domain.Article) error {
	done, err := s.chunkRepo.ExistsByArticleID(ctx, article.ID)
	if err != nil {
		return fmt.Errorf("kiểm tra chunk: %w", err)
	}
	if done {
		return nil
	}
	rc, err := s.storage.Download(ctx, article.StorageKey)
	if err != nil {
		return fmt.Errorf("tải blob: %w", err)
	}
	pages, err := extractor.ExtractPages(ctx, article.StorageKey, rc, s.ocr)
	_ = rc.Close()
	if err != nil {
		if errors.Is(err, extractor.ErrUnsupported) {
			return fmt.Errorf("loại file chưa xử lý: %w", err)
		}
		return fmt.Errorf("trích text: %w", err)
	}
	var chunks []*domain.ArticleChunk
	for _, pg := range pages {
		if strings.TrimSpace(pg.Text) == "" {
			continue
		}
		// Lỗi splitter (tầng semantic gọi Ollama) là transient — trả về để
		// caller backoff, không failed oan như lỗi file hỏng (permanent).
		parts, err := s.splitter.Split(ctx, pg.Text)
		if err != nil {
			return fmt.Errorf("cắt chunk: %w", err)
		}
		for _, p := range parts {
			// PointID sinh 1 lần duy nhất ở đây (UUIDv7, sort được theo thời
			// gian) rồi lưu DB — phase 2 chỉ đọc lại nên embed chạy lại bao
			// nhiêu lần cũng upsert đúng point cũ, không trùng.
			pointID, err := utils.NewUUIDv7()
			if err != nil {
				return fmt.Errorf("sinh point id: %w", err)
			}
			chunks = append(chunks, &domain.ArticleChunk{
				ArticleID:  article.ID,
				ChunkIndex: len(chunks),
				Content:    p,
				PointID:    pointID,
				PageNum:    pg.Num,
			})
		}
	}
	if len(chunks) == 0 {
		return fmt.Errorf("%w: không còn trang nào có chữ để chunk", extractor.ErrInvalid)
	}
	if err := s.chunkRepo.CreateBatch(ctx, chunks); err != nil {
		return fmt.Errorf("lưu chunk: %w", err)
	}
	// User có thể xóa bài trong lúc worker đang cắt — bài chết rồi thì dọn
	// chunk vừa tạo ngay (purge không thấy chunk mồ côi của bài đã xóa hẳn),
	// service đánh done sau đó cũng chỉ là no-op trên row đã mất.
	alive, err := s.articleRepo.IsAlive(ctx, article.ID)
	if err != nil {
		return fmt.Errorf("kiểm tra bài còn sống: %w", err)
	}
	if !alive {
		logs.Infow("chunk: bài bị xóa giữa chừng, đã dọn chunk vừa tạo", "trace_id", traceID, "article_id", article.ID)
		det := worker.Detach(ctx)
		defer det.Close()
		if derr := s.chunkRepo.DeleteByArticleID(det.Ctx, article.ID); derr != nil {
			return fmt.Errorf("dọn chunk bài đã xóa: %w", derr)
		}
		return nil
	}
	logs.Infow("chunk: xong 1 bài", "trace_id", traceID, "article_id", article.ID, "pages", len(pages), "chunks", len(chunks))
	return nil
}
