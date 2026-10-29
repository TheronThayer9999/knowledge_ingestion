package services

import (
	"context"
	"fmt"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/domain"
)

// IIndexReconcileService lưới chắn cho tai biến "DB báo done nhưng kho vector
// trống". Xảy ra thật khi Qdrant mất collection: ensureCollection drop/recreate
// lúc shape vector đổi (đổi model embedding → đổi dim), còn articles vẫn giữ
// index_done_chunks > 0 và index_status = done — worker không bao giờ quay lại
// đó nữa, search âm thầm trả kết quả thiếu.
//
// Phase 1 đã có guard verifyVectors chặn bài MỚI thành ma; service này chữa
// phần đã lỡ thành ma TRƯỚC đó (và mọi losses do người vận hành xoá collection
// tay). Hội tụ khi chạy lại nên quét bao nhiêu lần cũng an toàn.
type IIndexReconcileService interface {
	// ScanGhosts ĐỌC KHÔNG: liệt kê bài index done mà Qdrant không còn point
	// nào, kèm hai con số để so (tổng point vs tổng chunk DB claim). Không ghi
	// gì nên endpoint consistency gọi thoải mái.
	ScanGhosts(ctx context.Context) (*GhostReport, error)
	// Reconcile scan rồi đưa bài ma về hàng đợi index (checkpoint = 0) để worker
	// tự chạy lại từ article_pages — không tốn OCR lần nào. Trả số bài đã requeue.
	Reconcile(ctx context.Context) (int, error)
}

// GhostReport là kết quả 1 lượt quét. Truncated = true nghĩa là chưa quét hết
// (chạm trần RECONCILE_MAX_ARTICLES) nên số ma còn có thể lớn hơn — nói rõ để
// người vận hành không kết luận "hệ thống sạch" từ một lượt cắt cụt.
type GhostReport struct {
	Scanned   int
	Ghosts    []*domain.Article
	Points    int64
	Claimed   int64
	Truncated bool
}

type indexReconcileService struct {
	articles domain.ArticleAdminQueue
	vectors  domain.IVectorStore
}

func NewIndexReconcileService(articles domain.ArticleAdminQueue, vectors domain.IVectorStore) IIndexReconcileService {
	return &indexReconcileService{articles: articles, vectors: vectors}
}

// ScanGhosts duyệt bài done theo lô, hỏi Qdrant từng bài một.
//
// Vì sao không so bằng MỘT phép đếm toàn collection: kho có 2000 bài nhưng 1 bài
// mất point thì tổng vẫn gần đúng, không chỉ ra được bài nào để chữa. Count theo
// bài mới là đơn vị hành động được — nhưng phải có trần, nên mới cần
// RECONCILE_MAX_ARTICLES.
//
// Phân trang theo OFFSET trên cột index_status không ổn định khi chạy song song:
// bài đang done mà bị requeue giữa hai lô thì dãy co lại, lô sau có thể nhảy
// qua mất vài bài. Không sao — job chạy định kỳ, bài bị nhảy vẫn được quét ở kỳ
// sau, và mọi bước đều idempotent nên quét lại bài đã chữa cũng không ghi gì sai.
func (s *indexReconcileService) ScanGhosts(ctx context.Context) (*GhostReport, error) {
	points, err := s.vectors.CountCollection(ctx)
	if err != nil {
		return nil, err
	}
	report := &GhostReport{Points: points}
	for offset := 0; ; offset += constants.RECONCILE_PAGE_SIZE {
		if report.Scanned >= constants.RECONCILE_MAX_ARTICLES {
			report.Truncated = true
			break
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		batch, err := s.articles.ListByIndexStatus(ctx, domain.QueueDone,
			constants.RECONCILE_PAGE_SIZE, offset)
		if err != nil {
			return nil, fmt.Errorf("liệt kê bài đã index done: %w", err)
		}
		if len(batch) == 0 {
			return report, nil
		}
		s.checkBatch(ctx, report, batch)
		if len(batch) < constants.RECONCILE_PAGE_SIZE {
			return report, nil
		}
	}
	return report, nil
}

// checkBatch đếm point của từng bài trong lô. Bài chưa có chunk nào (IndexDoneChunks
// = 0) được bỏ qua: "done 0 chunk" là bài rỗng chữ, không phải bài mất vector —
// chữa nó sẽ requeue vô ích.
//
// Lỗi đếm ở 1 bài chỉ warn + nhảy qua, không dựng cả lượt quét: đếm không được
// là chuyện Qdrant chập chờn, còn quyết định requeue cần bằng chứng rõ ràng là
// "collection trả về 0 point".
func (s *indexReconcileService) checkBatch(ctx context.Context, report *GhostReport, batch []*domain.Article) {
	for _, article := range batch {
		if report.Scanned >= constants.RECONCILE_MAX_ARTICLES {
			report.Truncated = true
			return
		}
		report.Scanned++
		if article.IndexDoneChunks <= 0 {
			continue
		}
		report.Claimed += int64(article.IndexDoneChunks)
		n, err := s.vectors.CountByArticle(ctx, article.ID)
		if err != nil {
			logs.Warnw("reconcile: không đếm được point, bỏ qua bài",
				"article_id", article.ID, "name", article.Name, "error", err)
			continue
		}
		if n > 0 {
			continue
		}
		report.Ghosts = append(report.Ghosts, article)
	}
}

// Reconcile chữa bằng đúng cái nút admin vẫn bấm (ResetQueue), chỉ khác chỗ
// userID lấy từ CHÍNH row đang xét chứ không phải từ token: id và owner cùng
// một nguồn nên bộ lọc user_id của repo vẫn khớp — không cần câu SQL bỏ chặn
// quyền, và cũng không có đường nào cho client đặt tên bài của người khác.
func (s *indexReconcileService) Reconcile(ctx context.Context) (int, error) {
	report, err := s.ScanGhosts(ctx)
	if err != nil {
		return 0, err
	}
	if len(report.Ghosts) == 0 {
		logs.Infow("reconcile vector: không có bài mất vector",
			"scanned", report.Scanned, "points", report.Points,
			"claimed_chunks", report.Claimed, "truncated", report.Truncated)
		return 0, nil
	}
	// WARN chứ không INFO: đây là tai biến, phải lộ trên log kể cả khi log_level
	// đang để mức cao hơn debug.
	logs.Warnw("reconcile vector: bài done nhưng kho trống, đưa về hàng đợi index",
		"ghosts", len(report.Ghosts), "scanned", report.Scanned,
		"points", report.Points, "claimed_chunks", report.Claimed,
		"truncated", report.Truncated, "article_ids", ghostIDs(report.Ghosts))

	n, err := s.requeueByOwner(ctx, report.Ghosts)
	if err != nil {
		return int(n), err
	}
	logs.Infow("reconcile vector: đã đưa về hàng đợi index", "requeued", n)
	return int(n), nil
}

// requeueByOwner gom bài theo chủ rồi reset từng chủ một lô. Gom theo owner vì
// ResetQueue nhận danh sách id của MỘT user; sai là 1 UPDATE, không phải N.
//
// ResetQueue cũng đưa compile_status về pending — không tốn LLM: cache Redis
// khóa theo (model|tên mục|nội dung) nên compile lại toàn mục y nguyên là
// toàn bộ cache hit.
func (s *indexReconcileService) requeueByOwner(ctx context.Context, ghosts []*domain.Article) (int64, error) {
	byOwner := make(map[int64][]int64)
	for _, article := range ghosts {
		byOwner[article.UserID] = append(byOwner[article.UserID], article.ID)
	}
	var total int64
	var firstErr error
	for userID, ids := range byOwner {
		n, err := s.articles.ResetQueue(ctx, ids, userID)
		if err != nil {
			logs.Error(err, "reconcile vector: không requeue được chủ này, kỳ sau thử lại",
				"user_id", userID, "articles", len(ids))
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		total += n
	}
	return total, firstErr
}

func ghostIDs(ghosts []*domain.Article) []int64 {
	ids := make([]int64, 0, len(ghosts))
	for _, article := range ghosts {
		ids = append(ids, article.ID)
	}
	return ids
}
