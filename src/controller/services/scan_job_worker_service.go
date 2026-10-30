package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/scanocr"
	"knowledge_ingestion/src/common/storage"
	"knowledge_ingestion/src/controller/services/internal/worker"
	"knowledge_ingestion/src/domain"

	"github.com/go-pdf/fpdf"
)

// IScanJobWorkerService xu ly hang doi OCR quet theo trang (worker). Khong qua HTTP.
type IScanJobWorkerService interface {
	ProcessPending(ctx context.Context) (int, error)
	ReleaseUnfinished(ctx context.Context) (int64, error)
}

type scanJobWorkerService struct {
	repo domain.IOCRJobRepository
	store storage.IStorage
	ocr  scanocr.Config
	opt  ScanJobOptions
}

func NewScanJobWorkerService(
	repo domain.IOCRJobRepository,
	store storage.IStorage,
	ocr scanocr.Config,
	opt ScanJobOptions,
) IScanJobWorkerService {
	return &scanJobWorkerService{repo: repo, store: store, ocr: ocr.Normalize(), opt: opt}
}

type savedScanWord struct {
	T string  `json:"t"`
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
	C float64 `json:"c"`
}

func (s *scanJobWorkerService) ProcessPending(ctx context.Context) (int, error) {
	traceID, _ := worker.EnsureTraceID(ctx)
	claimed, err := s.repo.ClaimPages(ctx, constants.SCAN_CLAIM_PAGES, constants.SCAN_PAGE_LEASE)
	if err != nil {
		return 0, err
	}
	if len(claimed) == 0 {
		return 0, nil
	}
	logs.Infow("scan-worker: claim trang", "trace_id", traceID, "count", len(claimed))

	processed := 0
	jobs := make(map[int64]*domain.OCRJob)
	for _, cp := range claimed {
		jobs[cp.Job.ID] = cp.Job
		if err := s.processOne(ctx, cp); err != nil {
			_, _ = s.repo.FailPage(ctx, cp.Job.ID, cp.Page.PageNum, err.Error(),
				s.ocr.MaxAttempts, constants.SCAN_PAGE_BACKOFF)
			logs.Warnw("scan-worker: trang loi", "trace_id", traceID,
				"job_id", cp.Job.ID, "page", cp.Page.PageNum, "error", err)
		} else {
			processed++
		}
	}

	// Job mà MỌI trang đã skipped (PDF đã có text layer) không có trang pending
	// để claim — thêm vào danh sách để dựng PDF và chốt done thay vì kẹt mãi.
	if ready, err := s.repo.ListReadyToBuild(ctx); err == nil {
		for _, j := range ready {
			if _, ok := jobs[j.ID]; !ok {
				jobs[j.ID] = j
			}
		}
	}

	for id, job := range jobs {
		prog, err := s.repo.SyncCounts(ctx, id)
		if err != nil || prog == nil {
			continue
		}
		if prog.Pending == 0 && prog.Running == 0 && job.Status != domain.QueueDone {
			if err := s.buildOutput(ctx, job); err != nil {
				_ = s.repo.MarkFailed(ctx, id, "scan.build_failed", err.Error())
				logs.Warnw("scan-worker: build pdf loi", "trace_id", traceID, "job_id", id, "error", err)
			}
		}
	}
	return processed, nil
}

func (s *scanJobWorkerService) processOne(ctx context.Context, cp *domain.ClaimedPage) error {
	job := cp.Job
	page := cp.Page
	srcPath, cleanup, err := s.downloadSource(ctx, job)
	if err != nil {
		return err
	}
	defer cleanup()

	ext := strings.ToLower(filepath.Ext(job.SourceName))
	var imgBytes []byte
	if ext == ".pdf" {
		imgBytes, err = scanocr.RenderPage(ctx, s.ocr, srcPath, page.PageNum, scanocr.RenderOptions{
			DPI:  s.ocr.RenderDPI,
			JPEG: false,
		})
		if err != nil {
			return fmt.Errorf("render trang: %w", err)
		}
	} else {
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("doc anh: %w", err)
		}
		imgBytes = data
	}

	engine, err := scanocr.NewEngine(s.ocr)
	if err != nil {
		return fmt.Errorf("engine: %w", err)
	}
	pageInfo, err := engine.Recognize(ctx, scanocr.ImageInfo{
		Page:      page.PageNum,
		Extension: ".png",
		DPI:       s.ocr.RenderDPI,
	}, imgBytes)
	if err != nil {
		return fmt.Errorf("ocr: %w", err)
	}

	var saved []savedScanWord
	for _, line := range pageInfo.Lines {
		for _, w := range line.Words {
			nb := w.Box.NormalizedBox(pageInfo.WidthPx, pageInfo.HeightPx)
			saved = append(saved, savedScanWord{
				T: w.Text,
				X: nb.X,
				Y: nb.Y,
				W: nb.W,
				H: nb.H,
				C: w.Conf,
			})
		}
	}
	raw, _ := json.Marshal(saved)
	stats := pageInfo.Stats(float64(s.ocr.MinConfWord))
	if err := s.repo.SavePageWords(ctx, job.ID, page.PageNum, string(raw), pageInfo.Text(), stats.MeanConf, stats.LowConf); err != nil {
		return fmt.Errorf("save words: %w", err)
	}
	return nil
}

// buildOutput chốt job thành tệp kết quả. KHÔNG dựng lại PDF và KHÔNG giữ ảnh
// trang nào trong RAM:
//   - nguồn PDF: output CHÍNH LÀ blob nguồn (`MarkDone` trỏ thẳng SourceKey) —
//     không render, không copy, không tốn thêm dung lượng;
//   - nguồn JPG/PNG: bọc đúng MỘT ảnh vào PDF một trang, khổ trang suy từ pixel
//     + DPI nên ảnh không bị kéo giãn.
//
// Trước đây hàm này render lại MỌI trang rồi ghép một PDF nhiều trang có lớp chữ
// ẩn: file 300 trang là 300 ảnh + fpdf + buffer cùng nằm trong RAM, nhân với số
// job song song là tràn bộ nhớ. Bỏ lớp chữ ẩn làm bước đó biến mất hẳn.
func (s *scanJobWorkerService) buildOutput(ctx context.Context, job *domain.OCRJob) error {
	ext := strings.ToLower(filepath.Ext(job.SourceName))
	if ext == ".pdf" {
		return s.repo.MarkDone(ctx, job.ID, job.SourceKey)
	}
	srcPath, cleanup, err := s.downloadSource(ctx, job)
	if err != nil {
		return err
	}
	defer cleanup()
	pdfBytes, err := imagePagePDF(srcPath, ext, s.ocr.RenderDPI)
	if err != nil {
		return err
	}
	outKey := scanOutputKey(job.ID)
	if err := s.store.Upload(ctx, outKey, bytes.NewReader(pdfBytes), int64(len(pdfBytes)), "application/pdf"); err != nil {
		return fmt.Errorf("upload pdf: %w", err)
	}
	if err := s.repo.MarkDone(ctx, job.ID, outKey); err != nil {
		return fmt.Errorf("mark done: %w", err)
	}
	return nil
}

// imagePagePDF bọc MỘT ảnh vào PDF một trang. Khổ trang suy từ pixel ảnh và DPI
// để bản in ra đúng tỉ lệ; pixel đọc bằng DecodeConfig (không giải mã toàn ảnh),
// fpdf tự đọc bytes lúc nhúng nên bộ nhớ chỉ là một ảnh + một tệp PDF.
//
// fpdf chỉ nhúng được JPEG/PNG — đó cũng là lý do tab Quét không nhận WEBP.
func imagePagePDF(path, ext string, dpi int) ([]byte, error) {
	if dpi <= 0 {
		dpi = 150
	}
	wPx, hPx, err := measureImage(path)
	if err != nil {
		return nil, err
	}
	if wPx <= 0 || hPx <= 0 {
		wPx, hPx = 1240, 1754 // A4 ở 150DPI khi không đọc được khổ ảnh
	}
	wPt := float64(wPx) * 72 / float64(dpi)
	hPt := float64(hPx) * 72 / float64(dpi)
	imgOpts := fpdf.ImageOptions{ImageType: "JPG"}
	if strings.EqualFold(ext, ".png") {
		imgOpts.ImageType = "PNG"
	}

	pdf := fpdf.NewCustom(&fpdf.InitType{UnitStr: "pt"})
	pdf.SetMargins(0, 0, 0)
	pdf.SetAutoPageBreak(false, 0)
	pdf.SetCompression(true)
	pdf.AddPageFormat("P", fpdf.SizeType{Wd: wPt, Ht: hPt})
	pdf.RegisterImageOptions(path, imgOpts)
	pdf.ImageOptions(path, 0, 0, wPt, hPt, false, imgOpts, 0, "")

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("bọc ảnh vào PDF: %w", err)
	}
	if buf.Len() == 0 {
		return nil, fmt.Errorf("bọc ảnh vào PDF: kết quả rỗng")
	}
	return buf.Bytes(), nil
}

func (s *scanJobWorkerService) downloadSource(ctx context.Context, job *domain.OCRJob) (string, func(), error) {
	return downloadScanTemp(ctx, s.store, job.SourceKey)
}

func (s *scanJobWorkerService) ReleaseUnfinished(ctx context.Context) (int64, error) {
	return s.repo.ReleaseUnfinished(ctx)
}
