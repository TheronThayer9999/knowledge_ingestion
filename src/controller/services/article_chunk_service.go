package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"knowledge_ingestion/src/common/chunker"
	"knowledge_ingestion/src/common/extractor"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/storage"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/domain"
)

const (
	// ChunkInterval là nhịp worker quét bài chưa chunk — export để worker lấy
	// làm chu kỳ ticker: 1 nguồn sự thật duy nhất như PurgeGracePeriod.
	ChunkInterval = 30 * time.Second
	// chunkClaimLimit số bài hốt mỗi kỳ — claim là rẻ (1 transaction), làm
	// không hết thì lease giữ bài lại, kỳ sau hốt tiếp.
	chunkClaimLimit = 6
	// chunkPoolSize số bài chunk song song — mỗi bài giữ đồng thời: blob raw
	// (trần 100MB) + text/XML giải nén (trần 80MB) + text rune khi split (text
	// 20MB ≈ 80MB rune) + ảnh render/OCR, nên worst-case ~300MB/bài → pool 3
	// ≈ 1GB nếu 3 file kịch trần cùng lúc. File thật (500 trang ≈ 5-15MB)
	// chỉ tốn vài chục MB/bài; đặt pool 3 là cân bằng throughput/RAM cho
	// container vừa, container nhỏ thì giảm số này.
	chunkPoolSize = 3
	// chunkLease thời gian giữ claim — file 500 trang chunk trong vài phút là
	// cùng; worker crash thì quá lease bài tự đủ điều kiện cho lần hốt sau.
	chunkLease = 15 * time.Minute
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
}

func NewArticleChunkService(articleRepo domain.ArticleChunkQueue, chunkRepo domain.IArticleChunkRepository, storage storage.IStorage, ocr extractor.OCRConfig) IArticleChunkService {
	return &articleChunkService{articleRepo: articleRepo, chunkRepo: chunkRepo, storage: storage, ocr: ocr}
}

// ChunkPending claim 1 đợt bài rồi chunk song song trong pool giới hạn, trả
// số bài đã cắt xong. Bài nào lỗi cũng chỉ log + MarkChunkError (backoff,
// quá số lần thì failed), không chặn bài khác. Claim bằng SKIP LOCKED nên N
// worker cùng chạy không giẫm nhau.
//
// Mỗi lần gọi lấy trace_id runner đã gắn vào ctx gắn vào mọi dòng log — cùng
// quy ước với PurgeDeleted, gọi trực tiếp (test, tool tay) thì tự sinh fallback.
func (s *articleChunkService) ChunkPending(ctx context.Context) (int, error) {
	traceID, err := ensureTraceID(ctx)
	if err != nil {
		return 0, err
	}
	logs.Infow("chunk: bắt đầu kỳ quét", "trace_id", traceID)
	articles, err := s.articleRepo.ClaimChunkPending(ctx, chunkClaimLimit, chunkLease)
	if err != nil {
		return 0, err
	}
	var chunked atomic.Int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, chunkPoolSize)
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
	dbCtx, cancel := detachCtx(ctx)
	defer cancel()
	if err := s.chunkOne(ctx, article); err != nil {
		if ctx.Err() != nil {
			logs.Infow("chunk: job hết timeout, bỏ qua không đánh lỗi",
				"trace_id", traceID, "article_id", article.ID)
			return
		}
		attempts := article.ChunkAttempts + 1
		if extractor.IsPermanent(err) {
			// Blob immutable nên lỗi xác định (loại chưa hỗ trợ, file hỏng,
			// ảnh trắng) thử lại cũng vậy — failed luôn thay vì retry 10 lần.
			attempts = domain.MaxQueueAttempts
		}
		logs.Warnw("chunk: bỏ qua bài, thử lại theo backoff",
			"trace_id", traceID,
			"article_id", article.ID, "storage_key", article.StorageKey, "error", err)
		if merr := s.articleRepo.MarkChunkError(dbCtx, article.ID, attempts); merr != nil {
			logs.Error(merr, "chunk: không đánh dấu lỗi được", "trace_id", traceID, "article_id", article.ID)
		}
		return
	}
	if err := s.articleRepo.MarkChunkDone(dbCtx, article.ID); err != nil {
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
func (s *articleChunkService) chunkOne(ctx context.Context, article *domain.Article) error {
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
		for _, p := range chunker.Split(pg.Text, chunker.Option{}) {
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
		logs.Infow("chunk: bài bị xóa giữa chừng, đã dọn chunk vừa tạo", "article_id", article.ID)
		dbCtx, cancel := detachCtx(ctx)
		defer cancel()
		if derr := s.chunkRepo.DeleteByArticleID(dbCtx, article.ID); derr != nil {
			return fmt.Errorf("dọn chunk bài đã xóa: %w", derr)
		}
		return nil
	}
	logs.Infow("chunk: xong 1 bài", "article_id", article.ID, "pages", len(pages), "chunks", len(chunks))
	return nil
}
