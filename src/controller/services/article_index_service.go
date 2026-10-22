package services

import (
	"context"
	stderrors "errors"
	"fmt"
	"maps"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"knowledge_ingestion/src/common/chunker"
	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/extractor"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/controller/services/internal/worker"
	"knowledge_ingestion/src/domain"
)

// IArticleIndexService worker phase 1: đọc toàn văn bản đã trích ở phase 0,
// cắt chunk, embed rồi upsert Qdrant — chỉ worker gọi, không qua HTTP nên trả
// (int, error) thuần thay vì dtos.Result. Trả số bài đã index xong.
//
// VÌ SAO CHUNK VÀ EMBED GỘP LÀI 1 PHASE:
//  1. Không cần bảng trung gian nữa. Chunk sau khi tách phase extract chỉ còn
//     là CPU + INSERT — viết 1500 row rồi đọc lại chỉ để xoá là vô nghĩa, và
//     Qdrant đã giữ text + vector trong payload làm nguồn sự thật.
//  2. Không còn trạng thái trung gian "chunk xong nhưng chưa embed" — một bài
//     hoặc đã có đủ vector trên Qdrant, hoặc chưa, không có trạng thái nửa vời.
//  3. Checkpoint chỉ cần MỘT SỐ NGUYÊN (articles.index_done_chunks) thay cho
//     cả bảng.
//
// ĐIỀU KIỆN ĐỂ GỘP ĐƯỢC: PointID phải tất định (utils.NewPointID) để chạy
// lại ghi đè đúng point cũ chứ không nhân bản — xem hàm đó.
type IArticleIndexService interface {
	IndexPending(ctx context.Context) (int, error)
}

type articleIndexService struct {
	articleRepo domain.ArticleIndexQueue
	pageRepo    domain.IArticlePageRepository
	embedder    domain.Embedder
	vectors     domain.IVectorStore
	// baseSplitter chiến lược cắt nền (Eino recursive/semantic/manual) —
	// loader chọn theo config worker.chunker, dựng 1 lần rồi tái dùng.
	// Template (laws/table/...) bọc base này để áp chiến lược theo loại tài liệu.
	baseSplitter chunker.Chunker
	// NOTE: embed_cache đã bị loại bỏ (hit rate 99.99% count=1 → vô dụng).
	// Vector lưu trong Qdrant; rebuild dùng checkpoint index_done_chunks.
}

func NewArticleIndexService(
	articleRepo domain.ArticleIndexQueue,
	pageRepo domain.IArticlePageRepository,
	embedder domain.Embedder,
	vectors domain.IVectorStore,
	baseSplitter chunker.Chunker) IArticleIndexService {
	return &articleIndexService{
		articleRepo:  articleRepo,
		pageRepo:     pageRepo,
		embedder:     embedder,
		vectors:      vectors,
		baseSplitter: baseSplitter,
	}
}

// IndexPending claim 1 đợt bài rồi index song song trong pool giới hạn, trả
// số bài xong. Bài nào lỗi cũng chỉ log + MarkIndexError (backoff, quá số lần
// thì failed), không chặn bài khác. Claim bằng SKIP LOCKED nên N worker cùng
// chạy không giẫm nhau. Cùng quy ước trace_id với ExtractPending/PurgeDeleted.
func (s *articleIndexService) IndexPending(ctx context.Context) (int, error) {
	traceID, err := worker.EnsureTraceID(ctx)
	if err != nil {
		return 0, err
	}
	logs.Infow("index: bắt đầu kỳ quét", "trace_id", traceID)
	articles, err := s.articleRepo.ClaimIndexPending(ctx, constants.INDEX_CLAIM_LIMIT, constants.INDEX_LEASE)
	if err != nil {
		return 0, err
	}
	var indexed atomic.Int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, constants.INDEX_POOL_SIZE)
	// pending giữ bài đã claim nhưng CHƯA vào goroutine; xóa khi đã start.
	// Dùng map thay vì quét lại slice: vừa tự dedupe id, vừa lấy phần còn
	// lại bằng range keys — Go không có LINQ nên map là cách gọn nhất.
	pending := make(map[int64]struct{}, len(articles))
	for _, article := range articles {
		pending[article.ID] = struct{}{}
	}
loop:
	for _, article := range articles {
		if ctx.Err() != nil {
			break
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break loop
		}
		delete(pending, article.ID)
		wg.Add(1)
		go func(a *domain.Article) {
			defer wg.Done()
			defer func() { <-sem }()
			s.indexClaimed(ctx, traceID, a, &indexed)
		}(article)
	}
	wg.Wait()
	if ctx.Err() != nil && len(pending) > 0 {
		// maps.Keys + slices.Collect thay cho vòng for thủ công; DB chỉ nhận
		// ĐÚNG 1 UPDATE ... WHERE id IN (...) — không N+1.
		ids := slices.Collect(maps.Keys(pending))
		s.releaseClaims(ctx, traceID, ids)
	}
	n := int(indexed.Load())
	logs.Infow("index: xong 1 kỳ quét", "trace_id", traceID, "found", len(articles), "indexed", n)
	return n, nil
}

// indexClaimed xử lý 1 bài đã claim: lỗi thì MarkIndexError, xong thì
// MarkIndexDone. Đánh dấu bằng detached ctx để ghi được dù job hết timeout;
// job hết timeout (ctx hủy) thì bỏ qua không đánh lỗi oan.
func (s *articleIndexService) indexClaimed(ctx context.Context, traceID string, article *domain.Article, done *atomic.Int64) {
	// KHÔNG tạo dbCtx dùng chung ở đây: 1 bài 500 trang (~1500 chunk) gọi
	// Ollama hàng chục phút, ctx 30s tạo sớm chết trước khi tới lượt ghi.
	if err := s.indexOneSafe(ctx, traceID, article); err != nil {
		if ctx.Err() != nil {
			// Canceled = runner dừng (shutdown) → trả bài về pending ngay để
			// restart làm tiếp, không đốt attempts. DeadlineExceeded = job vượt
			// trần 45 phút → để lease reclaim, tránh retry nóng vô hạn.
			if stderrors.Is(ctx.Err(), context.Canceled) {
				s.releaseClaims(ctx, traceID, []int64{article.ID})
			} else {
				logs.Infow("index: job hết giờ, để lease trả bài",
					"trace_id", traceID, "article_id", article.ID)
			}
			return
		}
		attempts := article.IndexAttempts + 1
		// Lỗi vĩnh viễn (sai dim/model, request sai 4xx, sai contract nội
		// bộ) retry cũng vậy — failed luôn thay vì backoff 10 lần. Hỏi tính
		// vĩnh viễn qua domain interface để service không import infra (DIP).
		if stderrors.Is(err, errIndexContract) || s.embedder.IsPermanentError(err) || s.vectors.IsPermanentError(err) {
			attempts = utils.MaxQueueAttempts
			logs.Warnw("index: lỗi vĩnh viễn, failed luôn",
				"trace_id", traceID, "article_id", article.ID, "error", err)
		} else {
			logs.Warnw("index: bỏ qua bài, thử lại theo backoff",
				"trace_id", traceID, "article_id", article.ID, "error", err)
		}
		det := worker.Detach(ctx)
		defer det.Close()
		if merr := s.articleRepo.MarkIndexError(det.Ctx, article.ID, attempts); merr != nil {
			logs.Error(merr, "index: không đánh dấu lỗi được", "trace_id", traceID, "article_id", article.ID)
		}
		return
	}
	det := worker.Detach(ctx)
	defer det.Close()
	if err := s.articleRepo.MarkIndexDone(det.Ctx, article.ID); err != nil {
		logs.Error(err, "index: không đánh dấu done được", "trace_id", traceID, "article_id", article.ID)
		return
	}
	done.Add(1)
}

// indexOne cắt + embed + upsert 1 bài, đọc nội dung theo CỬA SỔ TRƯỢT.
//
// BA BƯỚC, khác hẳn mô hình cũ (cắt từng trang rời rồi ghi bảng trung gian):
//  1. Quét 1 lượt để chốt quyết định toàn file: bỏ dòng lặp header/footer,
//     khử chữ rải hay không (xem extractor.Cleaner). Lượt này CHỈ CỘNG SỐ,
//     không giữ text — nên file 5000 trang vẫn chỉ tốn vài chục KB.
//  2. Quét lượt 2 theo cửa sổ INDEX_PAGE_WINDOW trang: ghép thành văn bản
//     liền mạch rồi SplitBlocks TRONG CỬA SỔ. Nhờ vậy nhìn Điều/mục được
//     ở tầm vài trang thay vì bị cắt vụn ở từng trang như cách cũ.
//  3. Mỗi block cắt tại RANH GIỚI TRANG rồi mới split theo size — chunk
//     không bao giờ tràn 2 trang nên PageNum chính xác cho trích dẫn.
//
// Chunk đủ INDEX_BATCH_SIZE thì embed + upsert + ghi checkpoint rồi đi tiếp,
// nên bộ nhớ chỉ ôm 1 cửa sổ + 1 đợt, KHÔNG phụ thuộc kích thước file.
// releaseClaims trả 1 lô bài về pending khi job bị hủy do shutdown — 1 câu
// UPDATE, không N+1. Detached ctx vì ctx gốc đã cancel; WHERE ... processing
// trong repo bảo vệ bài đã kịp done.
func (s *articleIndexService) releaseClaims(ctx context.Context, traceID string, ids []int64) {
	if len(ids) == 0 {
		return
	}
	det := worker.Detach(ctx)
	defer det.Close()
	if err := s.articleRepo.ReleaseIndexClaims(det.Ctx, ids); err != nil {
		logs.Error(err, "index: không trả claim được, bài chờ hết lease",
			"trace_id", traceID, "articles", len(ids))
		return
	}
	logs.Infow("index: job bị hủy (shutdown), trả bài về pending",
		"trace_id", traceID, "articles", len(ids), "article_ids", ids)
}

// indexOneSafe bọc indexOne để panic trong goroutine con (không có
// gin.Recovery) không giết process: biến panic thành error để đường xử lý lỗi
// hiện có đánh dấu bài + backoff, thay vì kẹt processing đến hết lease.
func (s *articleIndexService) indexOneSafe(ctx context.Context, traceID string, article *domain.Article) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			logs.Error(fmt.Errorf("panic: %v", rec), "index: panic khi xử lý bài",
				"trace_id", traceID, "article_id", article.ID, "stack", string(debug.Stack()))
			err = fmt.Errorf("panic khi index bài %d: %v", article.ID, rec)
		}
	}()
	return s.indexOne(ctx, traceID, article)
}

func (s *articleIndexService) indexOne(ctx context.Context, traceID string, article *domain.Article) error {
	start := time.Now()
	// Pass 1: chốt quyết định toàn file.
	cleaner, err := s.buildCleaner(ctx, article)
	if err != nil {
		return err
	}
	// Checkpoint: đã upsert N chunk đầu. Chunk sinh lại tất định từ cùng
	// text + cùng cấu hình, và PointID tất định nên bỏ qua N chunk đầu là an
	// toàn — lần chạy sau chỉ làm nốt phần còn lại.
	resumeFrom := article.IndexDoneChunks

	batch := make([]*domain.VectorPoint, 0, constants.INDEX_BATCH_SIZE)
	total := resumeFrom
	// carry là tiêu đề mục nối xuyên: chunk kế tiếp không tìm thấy tiêu đề
	// thì thừa hưởng mục trước, nên chunk giữa Điều không bao giờ mù ngữ cảnh
	// (xem chunker.Annotate).
	carry := ""

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := s.upsertBatch(ctx, traceID, article, batch); err != nil {
			return err
		}
		batch = batch[:0]
		// Ghi checkpoint SAU khi Qdrant báo thành công: thứ tự
		// Qdrant-trước-DB-sau. Lỗi ở giữa thì kỳ sau làm lại đúng đợt đó,
		// upsert cùng PointID nên không sinh point trùng.
		//
		// total KHÔNG cộng thêm ở đây: vòng sinh chunk đã tăng sẵn khi append
		// vào batch, cộng thêm lần nữa là checkpoint vượt thật.
		det := worker.Detach(ctx)
		defer det.Close()
		if err := s.articleRepo.MarkIndexProgress(det.Ctx, article.ID, total); err != nil {
			return fmt.Errorf("ghi checkpoint: %w", err)
		}
		// Log mỗi đợt: file lớn chạy hàng chục phút, không có dòng nào thì
		// lúc chết không biết đang ở đâu.
		logs.Infow("index: tiến trình",
			"trace_id", traceID, "article_id", article.ID,
			"chunks", total, "elapsed_ms", time.Since(start).Milliseconds())
		return nil
	}

	// Chạy lại từ đầu khi chưa có checkpoint: dọn point cũ trước khi ghi đè,
	// để đổi cấu hình chunker (sinh NÊN chunk khác) không để lại đuôi thừa
	// trong Qdrant. Delete theo filter là idempotent, bài chưa có point nào
	// thì khớp 0 vẫn success.
	if resumeFrom == 0 {
		// Đừng bắt đầu xóa toàn bộ vector của bài nếu job đã bị hủy — thao
		// tác này dùng detached ctx nên sẽ chạy tiếp bất chấp shutdown.
		if err := ctx.Err(); err != nil {
			return err
		}
		det := worker.Detach(ctx)
		if err := s.vectors.DeleteByArticle(det.Ctx, article.ID); err != nil {
			det.Close()
			return fmt.Errorf("dọn vector cũ: %w", err)
		}
		det.Close()
	}

	afterSeq := 0
	piecesSeen := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		pages, err := s.pageRepo.ListPagesRange(ctx, article.ID, afterSeq, constants.INDEX_PAGE_WINDOW)
		if err != nil {
			return fmt.Errorf("đọc trang: %w", err)
		}
		if len(pages) == 0 {
			// Hết trang — đây mới là điều kiện dừng DUY NHẤT. Không được kết
			// luận "hết file" từ lượt đọc ngắn hơn INDEX_PAGE_WINDOW: cửa sổ
			// bị cắt ngắn vì chạm INDEX_WINDOW_MAX_CHARS vẫn trả về đủ trang,
			// và coi lượt ngắn là hết file thì âm thầm bỏ sót nửa tài liệu.
			break
		}
		win := newPageWindow()
		for _, p := range pages {
			win.add(p.PageNum, cleaner.Apply(p.Text))
			afterSeq = p.Seq + 1
			piecesSeen++
			// Cửa sổ vừa đủ rồi thì dừng nhét thêm: bảo đảm trần ký tự kể cả
			// với loại file 1-trang chứa cả file.
			if win.chars() >= constants.INDEX_WINDOW_MAX_CHARS {
				break
			}
		}
		for _, b := range chunker.SplitBlocks(win.text()) {
			for _, piece := range win.cuts(b.Start, b.End) {
				if strings.TrimSpace(piece.text) == "" {
					continue
				}
				// Lỗi splitter (tầng semantic gọi Ollama) là transient — trả
				// về để caller backoff, không failed oan như lỗi dữ liệu hỏng.
				// Chọn template theo loại tài liệu của bài (laws/table/...) —
				// bọc baseSplitter, fallback general = base.
				tmpl := chunker.NewTemplate(chunker.NormalizeType(article.DocType), s.baseSplitter)
				if tmpl == nil {
					tmpl = s.baseSplitter
				}
				if article.DocType == "" {
					tmpl = chunker.NewTemplate(chunker.DetectType(article.ContentType, article.Name), s.baseSplitter)
					if tmpl == nil {
						tmpl = s.baseSplitter
					}
				}
				parts, err := tmpl.Split(ctx, piece.text)
				if err != nil {
					return fmt.Errorf("cắt chunk: %w", err)
				}
				// Gán mục cho từng chunk: block có tiêu đề thì lấy đó làm gốc,
				// không thì thừa hưởng carry.
				base := carry
				if b.Heading != "" {
					base = b.Heading
				}
				annotated, carryNext := chunker.Annotate(parts, base)
				carry = carryNext
				for _, p := range annotated {
					// Chunk đã upsert ở lần chạy trước: bỏ qua, chỉ đếm để
					// index nối tiếp.
					if total < resumeFrom {
						total++
						continue
					}
					// Prepend tiêu đề mục (chunker.Annotate đã gán lúc cắt) để
					// vector + Qdrant payload luôn mang ngữ cảnh Điều — agent
					// attribute đúng mục, full-text "ĐIỀU 5" cũng trúng chunk
					// giữa Điều. Bản lưu trong payload là bản dùng thật.
					text := chunker.HeaderFor(p.Section) + p.Text
					batch = append(batch, &domain.VectorPoint{
						ID:         utils.NewPointID(article.ID, total),
						ArticleID:  article.ID,
						UserID:     article.UserID,
						CategoryID: article.CategoryID,
						ChunkIndex: total,
						PageNum:    piece.page,
						Section:    p.Section,
						Text:       text,
					})
					total++
					if len(batch) >= constants.INDEX_BATCH_SIZE {
						if err := flush(); err != nil {
							return err
						}
						// Bài bị xoá giữa chừng thì dừng ngay (đỡ tốn tiền
						// Ollama), dọn vector vừa làm để không còn rác mà purge
						// không thấy. Check mỗi đợt vì 1 bài 500 trang chạy
						// hàng chục phút — 1 SELECT rẻ hơn nhiều so với gọi
						// Ollama thừa.
						alive, aerr := s.ensureAlive(ctx, traceID, article)
						if aerr != nil {
							return aerr
						}
						if !alive {
							return nil
						}
					}
				}
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	if total == 0 {
		return fmt.Errorf("%w: không có trang nào ra chữ để chunk", extractor.ErrInvalid)
	}
	logs.Infow("index: xong 1 bài", "trace_id", traceID, "article_id", article.ID,
		"pieces", piecesSeen, "chunks", total,
		"elapsed_ms", time.Since(start).Milliseconds())
	return nil
}

// buildCleaner là pass 1 — quét mọi trang theo cửa sổ CHỈ ĐỂ CỘNG SỐ, không
// giữ text. Chính vì vậy RAM của pass này là hằng số dù file có bao nhiêu
// trang, và đây là chỗ duy nhất biết "cả file chữ rải hay sạch, có dòng lặp
// header/footer không".
func (s *articleIndexService) buildCleaner(ctx context.Context, article *domain.Article) (*extractor.Cleaner, error) {
	cleaner := extractor.NewCleaner(article.PageCount, extractor.RepeatOptions{})
	afterSeq := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pages, err := s.pageRepo.ListPagesRange(ctx, article.ID, afterSeq, constants.INDEX_PAGE_WINDOW)
		if err != nil {
			return nil, fmt.Errorf("quét trang: %w", err)
		}
		if len(pages) == 0 {
			return cleaner, nil
		}
		for _, p := range pages {
			cleaner.Scan(p.Text)
			afterSeq = p.Seq + 1
		}
	}
}

// upsertBatch gán vector cho 1 đợt chunk rồi upsert Qdrant.
//
// CACHE: trước khi gọi model, hỏi embed_cache xem chunk nào đã có vector.
// Chỉ embed phần THIẾU. Lý do phải có: embed là bước đắt nhất của cả
// pipeline — đo thật trên máy này bge-m3 chạy 7.77 chunk/s, còn đọc cache
// và upsert nhanh hơn hàng nghìn lần. Bài 100MB là ~4.5 triệu chunk ≈ 7 ngày
// compute, nên gọi lại model trên chunk đã tính là lãng phí không chấp
// nhận được. Còn nếu không có cache thì mỗi lần worker chết giữa chừng,
// mỗi lần đổi chunker, mỗi lần rebuild vector là một lần trả tiền lại
// toàn bộ.
//
// Cache KHÔNG quyết định tính đúng của kết quả — chỉ quyết định có gọi
// model hay không. Kết quả vector cuối cùng là của cùng text + cùng model.
func (s *articleIndexService) upsertBatch(ctx context.Context, traceID string, article *domain.Article, batch []*domain.VectorPoint) error {
	texts := make([]string, 0, len(batch))
	for _, p := range batch {
		texts = append(texts, p.Text)
	}
	// Dual embedding: title vector = embed tên bài (1 lần/đợt, tái dùng cho
	// mọi chunk trong đợt). Lỗi embed title không fail job — degrade về
	// content-only (title vector = content vector).
	titleVec, terr := s.embedder.Embed(ctx, article.Name)
	if terr != nil {
		logs.Error(terr, "index: embed title lỗi, dùng content vector thay thế", "article_id", article.ID)
		titleVec = nil
	}
	// Embed toàn bộ đợt (cache đã bỏ — hit rate 99.99% count=1 → vô dụng).
	// Vector lưu trong Qdrant; rebuild dùng checkpoint index_done_chunks.
	logs.Infow("ollama: gọi embed", "trace_id", traceID, "article_id", article.ID,
		"texts", len(texts), "model", s.embedder.ModelName())
	embedStart := time.Now()
	vectors, err := s.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		// retry transient
		if rerr := worker.WithRetry(ctx, constants.INDEX_CALL_ATTEMPTS, constants.INDEX_CALL_BASE_DELAY,
			func(ctx context.Context) error {
				var e error
				vectors, e = s.embedder.EmbedBatch(ctx, texts)
				return e
			}); rerr != nil {
			return fmt.Errorf("gọi embed: %w", rerr)
		}
	}
	logs.Infow("ollama: embed xong", "trace_id", traceID, "article_id", article.ID,
		"texts", len(texts), "elapsed_ms", time.Since(embedStart).Milliseconds())
	if len(vectors) != len(texts) {
		return fmt.Errorf("%w: embed trả %d vector cho %d text", errIndexContract, len(vectors), len(texts))
	}
	for i, p := range batch {
		if len(vectors[i]) != s.embedder.Dimension() {
			return fmt.Errorf("%w: vector chunk %d sai dim", errIndexContract, i)
		}
		p.Vector = vectors[i]
		if titleVec != nil {
			p.TitleVector = titleVec
		} else {
			p.TitleVector = vectors[i]
		}
	}
	if err := worker.WithRetry(ctx, constants.INDEX_CALL_ATTEMPTS, constants.INDEX_CALL_BASE_DELAY,
		func(ctx context.Context) error {
			return s.vectors.UpsertPoints(ctx, batch)
		}); err != nil {
		return fmt.Errorf("upsert qdrant: %w", err)
	}
	return nil
}

// lookupCache hỏi cache cho cả đợt chunk. Trả về:
//   - cached: map[hash]vector — chỉ chứa hash CÓ trong cache;
//   - missIdx: chỉ số trong texts cần gọi model;
//   - missTexts: text của các phần tử missIdx, đúng thứ tự.
//
// Cache không bắt buộc phải có: cache lỗi thì coi như toàn bộ miss và log,
// job vẫn chạy được. Một bộ cache hỏng không được làm hỏng việc index.
// ensureAlive kiểm tra bài còn sống — bài chết thì dọn vector vừa làm rồi trả
// (false, nil) để caller dừng mà không mark done. Lỗi DB (kể cả ctx hủy giữa
// chừng) trả về error để indexClaimed xử lý đúng: cancellation → release
// claim, lỗi thật → MarkIndexError. Trước đây hàm trả bool nên lỗi IsAlive
// (nhất là ctx.Canceled lúc shutdown) bị biến thành "false" rồi indexOne trả
// nil → bài bị mark done dù chưa index xong.
func (s *articleIndexService) ensureAlive(ctx context.Context, traceID string, article *domain.Article) (bool, error) {
	alive, err := s.articleRepo.IsAlive(ctx, article.ID)
	if err != nil {
		return false, fmt.Errorf("kiểm tra bài còn sống: %w", err)
	}
	if !alive {
		det := worker.Detach(ctx)
		defer det.Close()
		logs.Infow("index: bài bị xoá giữa chừng, đã dọn vector vừa làm",
			"trace_id", traceID, "article_id", article.ID)
		if derr := s.vectors.DeleteByArticle(det.Ctx, article.ID); derr != nil {
			logs.Error(derr, "index: không dọn được vector bài đã xoá", "trace_id", traceID, "article_id", article.ID)
		}
		return false, nil
	}
	return true, nil
}

// errIndexContract báo embedder trả thiếu vector (sai contract do service tự
// phát hiện) — retry vô ích nên failed luôn, cùng nhóm với lỗi vĩnh viễn của
// embedder/vector store (hỏi qua domain interface, không import infra).
var errIndexContract = stderrors.New("embed trả thiếu vector")

// ---- CỬA SỔ TRƯỢT ----
//
// pageWindow là vài trang liền nhau đã nối thành văn bản, kèm byte offset của
// từng trang để map offset của block (chunker.SplitBlocks trả về) về đúng số
// trang. Toàn bộ cấu trúc này chỉ dài bằng cửa sổ, KHÔNG bằng cả file — đó
// là điều giữ RAM của phase index không phụ thuộc kích thước tài liệu.
type pageWindow struct {
	sb     strings.Builder
	starts []int // offset đầu mừng trang trong text
	nums   []int // số trang tương ứng
	nChars int
}

// windowSep nối 2 trang trong cửa sổ — 2 dòng trống đúng như PDF thật, để
// findHeadings nhìn đầu dòng sau đó không dính chữ trang trước.
const windowSep = "\n\n"

func newPageWindow() *pageWindow {
	return &pageWindow{}
}

// add nối 1 trang vào cửa sổ và ghi lại offset đầu trang đó.
func (w *pageWindow) add(num int, text string) {
	if len(w.nums) > 0 {
		w.sb.WriteString(windowSep)
		w.nChars += len(windowSep)
	}
	w.starts = append(w.starts, w.sb.Len())
	w.nums = append(w.nums, num)
	w.sb.WriteString(text)
	w.nChars += len(text)
}

func (w *pageWindow) text() string { return w.sb.String() }
func (w *pageWindow) chars() int   { return w.nChars }

// piece là lát cắt của 1 block thuộc trang page.
type piece struct {
	page int
	text string
}

// cuts tách đoạn [lo, hi) của cửa sổ tại ranh giới trang — thay cho ranh giới
// trang của thiết kế cũ (cắt từng trang rời rồi ghép): block Điều trải qua 3
// trang vẫn là MỘT block nên không bị cắt vụn, nhưng mỗi chunk sinh ra vẫn
// nằm gọn trong 1 trang để PageNum chính xác cho trích dẫn.
func (w *pageWindow) cuts(lo, hi int) []piece {
	lo, hi = max(lo, 0), min(hi, w.sb.Len())
	if lo >= hi {
		return nil
	}
	// String() sao chép cả builder — gọi MỘT lần rồi cắt trên string, không
	// gọi trong vòng lặp (mỗi lần là một bản sao cỡ cửa sổ).
	full := w.text()
	var out []piece
	for i, start := range w.starts {
		end := w.sb.Len()
		if i+1 < len(w.starts) {
			end = w.starts[i+1] - len(windowSep)
		}
		if end <= lo {
			continue
		}
		if start >= hi {
			break
		}
		s, e := max(lo, start), min(hi, end)
		if e > s {
			out = append(out, piece{page: w.nums[i], text: full[s:e]})
		}
	}
	return out
}
