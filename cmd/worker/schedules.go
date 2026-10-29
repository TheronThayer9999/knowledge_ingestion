package main

import (
	"context"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/controller/services"
)

// schedulesFor khai báo toàn bộ job nền của worker — tách khỏi main() để main
// chỉ lo lắp fx, còn nhịp/thời gian chạy của từng job nằm cùng một chỗ và đọc
// được như một bảng. Thêm job = append 1 dòng ở đây (không phải sửa main).
//
// Mỗi job nói trần thời gian riêng: job nặng (OCR/embed file 500 trang) mà ăn
// mặc định jobRunTimeout 1 phút thì kỳ nào cũng bị cancel giữa chừng rồi làm
// lại từ đầu — đói vĩnh viễn.
//
// Runner chạy MỖI job 1 phát ngay lúc boot (xem Attach) nên chẳng job nào phải
// chờ đủ nhịp đầu tiên.
func schedulesFor(
	purge services.IArticlePurgeService,
	extract services.IArticleExtractService,
	index services.IArticleIndexService,
	compile services.IArticleCompileService,
	refreshJanitor services.IRefreshTokenJanitor,
	reconcile services.IIndexReconcileService,
	opts services.PurgeOptions,
) []Schedule {
	return []Schedule{
		{Name: "purge-articles", Interval: constants.PURGE_GRACE_PERIOD, Run: func(ctx context.Context) error {
			_, err := purge.PurgeDeleted(ctx)
			return err
		}},
		{Name: "trim-orphan-pages", Interval: opts.TrimInterval, Run: func(ctx context.Context) error {
			_, err := purge.TrimOrphanPages(ctx)
			return err
		}},
		// Timeout riêng vì bài 500 trang OCR mất ~17 phút — cancel giữa
		// chừng thì mất công. Nhờ EXTRACT_FLUSH_PAGES nên hết giờ chỉ
		// mất tối đa 1 lô trang, kỳ sau nhảy tiếp.
		{Name: "extract-articles", Interval: constants.EXTRACT_INTERVAL, Timeout: constants.EXTRACT_JOB_TIMEOUT, Run: func(ctx context.Context) error {
			_, err := extract.ExtractPending(ctx)
			return err
		}},
		// Phase index không còn OCR nên nhẹ hơn hẳn, nhưng bài lớn vẫn
		// gọi Ollama ~47 lần nên vẫn cần timeout riêng.
		{Name: "index-articles", Interval: constants.INDEX_INTERVAL, Timeout: constants.INDEX_JOB_TIMEOUT, Run: func(ctx context.Context) error {
			_, err := index.IndexPending(ctx)
			return err
		}},
		// Phase compile (P2.4 phase 1b): bài index done → LLM tóm tắt
		// từng mục → article_sections. Nhịp thưa hơn index vì call LLM
		// chậm; timeout riêng < lease để worker khác không reclaim oan.
		{Name: "compile-articles", Interval: constants.COMPILE_INTERVAL, Timeout: constants.COMPILE_JOB_TIMEOUT, Run: func(ctx context.Context) error {
			_, err := compile.CompilePending(ctx)
			return err
		}},
		// Dọn refresh token hết hạn/bị thu hồi — bảng nhỏ nhưng không dọn
		// thì phình mãi theo số lần đăng nhập.
		{Name: "purge-refresh-tokens", Interval: constants.REFRESH_TOKEN_JANITOR_INTERVAL, Run: func(ctx context.Context) error {
			_, err := refreshJanitor.PurgeExpired(ctx)
			return err
		}},
		// Lưới chắn mất vector: bài DB báo done nhưng Qdrant không còn point
		// (collection bị drop/recreate) thì đưa về hàng đợi index, chạy lại từ
		// article_pages — không tốn OCR lần nào. Quét theo bài nên nhịp thưa;
		// guard verifyVectors trong phase 1 mới chặn phát sinh thêm.
		{Name: "reconcile-vectors", Interval: constants.RECONCILE_INTERVAL, Timeout: constants.RECONCILE_TIMEOUT, Run: func(ctx context.Context) error {
			_, err := reconcile.Reconcile(ctx)
			return err
		}},
	}
}
