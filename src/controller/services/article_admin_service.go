package services

import (
	"context"
	"net/http"
	"time"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/domain"
)

// IArticleAdminService vận hành hàng đợi tri thức — HTTP và gRPC cùng gọi,
// worker không đụng. Mọi nghiệp vụ nặng vẫn do worker làm, service này chỉ
// đưa bài về pending để worker hốt lại, hoặc báo cáo trạng thái hàng đợi.
type IArticleAdminService interface {
	// RebuildVectors đưa bài về hàng đợi index từ đầu (Qdrant mất collection,
	// chuyển cụm mới, vector nghi hỏng). Thứ tự vector-trước-DB-sau: xóa
	// vector cũ rồi mới reset status để worker không chen ngang làm dở.
	// Lỗi giữa chừng thì fail rõ — gọi lại là hội tụ vì mọi bước idempotent.
	//
	// KHÔNG đụng phase extract: nội dung đã trích nằm ở article_pages nên
	// index lại không tốn OCR lần nào — đó là lý do tách 2 phase.
	RebuildVectors(ctx context.Context, dto *dtos.RebuildVectorsRequest) dtos.Result[*dtos.RebuildVectorsResponse]
	// ResetExtract xoá trang đã trích rồi đưa phase extract về pending để
	// worker OCR lại từ blob — dùng khi muốn cải thiện chất lượng trích (đổi
	// renderer OCR, đổi chính sách OCR). KHÔNG đụng phase index: vector đã có
	// vẫn dùng được cho tới khi index chạy lại.
	ResetExtract(ctx context.Context, dto *dtos.ResetScopeRequest) dtos.Result[*dtos.ResetScopeResponse]
	// QueueStatus vẽ toàn cảnh hàng đỏi 2 phase + bài đã xóa mềm chờ janitor
	// — READ-ONLY, không ghi gì nên gọi lại bao nhiêu lần cũng an toàn.
	QueueStatus(ctx context.Context, limit int) dtos.Result[*dtos.QueueStatusResponse]
}

type articleAdminService struct {
	articleRepo domain.ArticleAdminQueue
	pageRepo    domain.IArticlePageRepository
	vectors     domain.IVectorStore
	currentUser middlewares.ICurrentUser
}

func NewArticleAdminService(articleRepo domain.ArticleAdminQueue, pageRepo domain.IArticlePageRepository, vectors domain.IVectorStore, currentUser middlewares.ICurrentUser) IArticleAdminService {
	return &articleAdminService{articleRepo: articleRepo, pageRepo: pageRepo, vectors: vectors, currentUser: currentUser}
}

// RebuildVectors đưa bài về hàng đợi index từ đầu. Thứ tự 3 bước có chủ
// đích — mỗi bước là một bất biến, xáo bước nào là hỏng:
//
//  1. Resolve scope: đúng 1 trong article_ids / category_id, chỉ bài của mình.
//  2. CHẶN QUYỀN — bắt buộc, xong mới được xóa gì. Bước 3 xóa theo
//     article_id mà không có user_id, nếu không chặn trước thì client chỉ
//     cần đoán ra id người khác là xóa sạch vector của họ.
//  3. Xóa vector (theo lô) rồi ResetQueue để worker hốt lại.
//
// Không có transaction xuyên hệ thống (Qdrant không có transaction —
// cùng lý do purge chọn thứ tự blob-trước-row-sau), nên mục tiêu là HỘI TỤ
// KHI CHẠY LẠI chứ không phải atomic: mọi bước đều idempotent nên gọi lại
// cùng scope là ra kết quả đúng. Vì vậy bước nào lỗi thì log kèm trace + số
// id để biết chạy lại scope nào, thay vì im lặng trả 500.
func (s *articleAdminService) RebuildVectors(ctx context.Context, dto *dtos.RebuildVectorsRequest) dtos.Result[*dtos.RebuildVectorsResponse] {
	userID, _ := s.currentUser.UserID(ctx)
	ids, err := s.resolveScope(ctx, userID, dto.ArticleIDs, dto.CategoryID)
	if err != nil {
		return dtos.Fail[*dtos.RebuildVectorsResponse](err)
	}
	// Dọn hàng loạt: 1 câu Qdrant + 1 câu Postgres thay vì 2 lời gọi mỗi bài,
	// nên cửa sổ lỗi giữa chừng thu hẹp từ "N bài nửa vời" còn "1 lần gọi".
	//
	// PointID nay TẤT ĐỊNH theo (article_id, chunk_index) nên xoá vector cũ
	// là BẮT BUỘC: nếu chỉ upsert lại, đợt cũ và đợt mới dùng cùng ID nên
	// ghi đè đúng — nhưng nếu cấu hình chunker đổi sinh NÊN chunk, phần dư
	// của lần cũ sẽ mồ côi và search trả trùng nội dung.
	if err := s.vectors.DeleteByArticles(ctx, ids); err != nil {
		logs.Error(err, "rebuild: xóa vector thất bại, chạy lại cùng scope để hội tụ",
			"trace_id", utils.TraceIDFromCtx(ctx), "articles", len(ids))
		return dtos.Fail[*dtos.RebuildVectorsResponse](err)
	}
	n, err := s.articleRepo.ResetQueue(ctx, ids, userID)
	if err != nil {
		return dtos.Fail[*dtos.RebuildVectorsResponse](err)
	}
	return dtos.Ok(&dtos.RebuildVectorsResponse{ResetArticles: n, ArticleIDs: ids})
}

// ResetExtract xoá sạch trang đã trích rồi đưa phase extract về pending để
// worker OCR lại từ blob.
//
// THỨ TỰ BẮT BUỘC — xoá trang TRƯỚC, đổi trạng thái SAU:
//
//	1. resolveScope: đúng 1 trong article_ids / category_id, chỉ bài của mình.
//	2. Xoá article_pages.
//	3. ResetExtract (extract_status = pending).
//
// Nếu đảo 2 và 3 thì sinh cửa sổ "trang cũ còn, extract đã pending": worker
// đọc thấy đã có trang nên bỏ qua OCR, đánh done với TRANG CŨ — lệnh này
// không trích lại gì mà lại làm hỏng tiến trình. Xoá trang trước khi đổi
// trạng thái thì lỗi giữa chừng chỉ có một hướng hậu quả an toàn (trang mất,
// worker trích lại từ đầu) — đúng nguyên tắc hội tụ mà RebuildVectors dùng.
//
// KHÔNG xoá vector: nội dung đã index vẫn dùng được cho search cho tới khi
// ai đó gọi rebuild-vectors. Muốn cả hai thì gọi lần lượt 2 endpoint.
func (s *articleAdminService) ResetExtract(ctx context.Context, dto *dtos.ResetScopeRequest) dtos.Result[*dtos.ResetScopeResponse] {
	userID, _ := s.currentUser.UserID(ctx)
	ids, err := s.resolveScope(ctx, userID, dto.ArticleIDs, dto.CategoryID)
	if err != nil {
		return dtos.Fail[*dtos.ResetScopeResponse](err)
	}
	if err := s.pageRepo.DeleteByArticles(ctx, ids); err != nil {
		logs.Error(err, "reset-extract: xoá trang thất bại, chạy lại cùng scope để hội tụ",
			"trace_id", utils.TraceIDFromCtx(ctx), "articles", len(ids))
		return dtos.Fail[*dtos.ResetScopeResponse](err)
	}
	n, err := s.articleRepo.ResetExtract(ctx, ids, userID)
	if err != nil {
		return dtos.Fail[*dtos.ResetScopeResponse](err)
	}
	logs.Infow("reset-extract: đã đưa về hàng đợi OCR lại",
		"trace_id", utils.TraceIDFromCtx(ctx), "user_id", userID, "articles", n)
	return dtos.Ok(&dtos.ResetScopeResponse{ResetArticles: n, ArticleIDs: ids})
}

// resolveScope gộp 2 thao tác admin cần: nới danh sách id từ category_id, và
// chặn quyền. Tách ra vì sai thứ tự ở đây là mất dữ liệu của người khác — cả
// hai endpoint đều xoá theo article_id mà KHÔNG kèm user_id, nên phải kiểm
// owner trước khi động vào bất cứ thứ gì.
//
// Lỗi trả về là *errors.Error (4xx) hoặc lỗi thô (500) — Render phân loại,
// nên caller chỉ cần dtos.Fail(err).
func (s *articleAdminService) resolveScope(ctx context.Context, userID int64, articleIDs []int64, categoryID int64) ([]int64, error) {
	ids := articleIDs
	if len(ids) == 0 {
		if categoryID <= 0 {
			// Cả 2 rỗng mà coi như "reset toàn bộ" thì 1 cú click xoá sạch
			// hàng đợi của cả hệ thống — chặn, caller phải nói rõ scope.
			return nil, errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest,
				"chỉ rõ article_ids hoặc category_id")
		}
		var err error
		ids, err = s.articleRepo.IDsByCategory(ctx, categoryID, userID)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return nil, errors.NewCustomHttpError(http.StatusNotFound, http.StatusNotFound,
				"danh mục không có bài viết")
		}
	}
	foreign, err := s.articleRepo.ForeignIDs(ctx, ids, userID)
	if err != nil {
		return nil, err
	}
	if len(foreign) > 0 {
		return nil, errors.NewCustomHttpError(http.StatusForbidden, http.StatusForbidden,
			"một số id không thuộc của bạn hoặc không tồn tại")
	}
	return ids, nil
}

// QueueStatus gom trạng thái đủ để biết đang tắc ở đâu mà không phải đọc
// log: 8 ô đếm 2 phase, danh sách bài đang processing (kèm lease để phân
// biệt worker sống với worker rớt) + failed + pending, và bài đã xóa mềm
// chia theo cửa sổ undo.
//
// Mốc thời gian lấy 1 lần rồi truyền xuống mọi phép so sánh (lease_expired,
// can_undo) — gọi time.Now() rải rác trong vòng lặp thì 2 dòng cạnh nhau
// lệch nhau vài giây, nhìn vô lý.
func (s *articleAdminService) QueueStatus(ctx context.Context, limit int) dtos.Result[*dtos.QueueStatusResponse] {
	now := time.Now()
	perList, _ := utils.NormalizePagination(limit, 0)

	stats, err := s.articleRepo.QueueStats(ctx)
	if err != nil {
		return dtos.Fail[*dtos.QueueStatusResponse](err)
	}
	// list là 2 repo method khác nhau (cột extract_* và index_*) — không dựng
	// tên cột động để không bao giờ phải nối chuỗi vào SQL. 3 nhóm bài lấy
	// tuần tự, mỗi nhóm theo thứ tự ưu tiên để nhìn 1 lần ra ngay bài nào
	// đang chạy, bài nào fail, bài nào đang chờ.
	list := func(index bool, status string) ([]*domain.Article, error) {
		if index {
			return s.articleRepo.ListByIndexStatus(ctx, status, perList, 0)
		}
		return s.articleRepo.ListByExtractStatus(ctx, status, perList, 0)
	}
	phase := func(index bool) (*dtos.QueuePhaseView, error) {
		view := &dtos.QueuePhaseView{
			Processing: []*dtos.QueueArticleItem{},
			Failed:     []*dtos.QueueArticleItem{},
			Pending:    []*dtos.QueueArticleItem{},
		}
		for _, group := range []struct {
			status string
			into   *[]*dtos.QueueArticleItem
		}{
			{domain.QueueProcessing, &view.Processing},
			{domain.QueueFailed, &view.Failed},
			{domain.QueuePending, &view.Pending},
		} {
			articles, err := list(index, group.status)
			if err != nil {
				return nil, err
			}
			*group.into = dtos.ToQueueArticleItems(articles, now, index)
		}
		if index {
			view.Counts = dtos.QueuePhaseCounts{
				Pending: stats.IndexPending, Processing: stats.IndexProcessing,
				Done: stats.IndexDone, Failed: stats.IndexFailed,
			}
		} else {
			view.Counts = dtos.QueuePhaseCounts{
				Pending: stats.ExtractPending, Processing: stats.ExtractProcessing,
				Done: stats.ExtractDone, Failed: stats.ExtractFailed,
			}
		}
		return view, nil
	}

	extractView, err := phase(false)
	if err != nil {
		return dtos.Fail[*dtos.QueueStatusResponse](err)
	}
	indexView, err := phase(true)
	if err != nil {
		return dtos.Fail[*dtos.QueueStatusResponse](err)
	}

	// Purge: phần còn trong cửa sổ undo (deleted_at >= now-grace) thì user
	// cứu được, phần quá grace thì janitor sẽ dọn — đúng bộ lọc
	// ListSoftDeleted của worker nên endpoint này hiển thị chính xác kỳ
	// quét sắp chạy, không phải ước lượng.
	grace := constants.PURGE_GRACE_PERIOD
	cutoff := now.Add(-grace)
	undoing, err := s.articleRepo.ListSoftDeletedSince(ctx, cutoff, perList)
	if err != nil {
		return dtos.Fail[*dtos.QueueStatusResponse](err)
	}
	ready, err := s.articleRepo.ListSoftDeleted(ctx, cutoff, perList)
	if err != nil {
		return dtos.Fail[*dtos.QueueStatusResponse](err)
	}

	return dtos.Ok(&dtos.QueueStatusResponse{
		Now:     now,
		Extract: extractView,
		Index:   indexView,
		Purge: &dtos.PurgeView{
			GracePeriodMin: int(grace.Minutes()),
			BatchSize:      constants.PURGE_BATCH_SIZE,
			Undoing:        dtos.ToPurgeItems(undoing, now, grace),
			ReadyToPurge:   dtos.ToPurgeItems(ready, now, grace),
		},
	})
}
