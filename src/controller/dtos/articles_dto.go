package dtos

import (
	"knowledge_ingestion/src/domain"
	"regexp"
	"strings"
	"time"
)

// Articles là tài liệu tri thức của từng user — owner lấy từ token,
// không nhận từ client. Mỗi article có đúng 1 nguồn: URL (link web bên
// ngoài) HOẶC StorageKey (object đã upload lên kho qua presign) — service
// bắt đúng 1, còn đây chỉ validate hình thức từng field.

// CreateArticleRequest tạo bài viết mới trong một danh mục của mình.
type CreateArticleRequest struct {
	URL         string `json:"url,omitempty" binding:"omitempty,url,max=2048" example:"https://example.com/docs/go-fx"`
	StorageKey  string `json:"storage_key,omitempty" binding:"omitempty,max=500" example:"uploads/550e8400-e29b-41d4-a716-446655440000.pdf"`
	Name        string `json:"name" binding:"required,min=1,max=255" example:"Giới thiệu Go Fx"`
	ContentType string `json:"content_type" binding:"required,max=100" example:"text/html"`
	DocType     string `json:"doc_type,omitempty" binding:"omitempty,oneof=general laws table paper book qa manual" example:"laws"`
	Description string `json:"description,omitempty" binding:"omitempty,max=1000" example:"Tài liệu về dependency injection trong Go"`
	CategoryID  int64  `json:"category_id" binding:"required,gt=0" example:"1"`
}

// UpdateArticleRequest sửa từng phần — field nil nghĩa là "không gửi",
// giữ nguyên giá trị cũ. Nguồn (URL/StorageKey) là identity nên không cho
// sửa — muốn đổi nguồn thì xóa tạo lại.
type UpdateArticleRequest struct {
	Name        *string `json:"name,omitempty" binding:"omitempty,min=1,max=255" example:"Giới thiệu Go Fx"`
	ContentType *string `json:"content_type,omitempty" binding:"omitempty,max=100" example:"text/html"`
	DocType     *string `json:"doc_type,omitempty" binding:"omitempty,oneof=general laws table paper book qa manual" example:"laws"`
	Description *string `json:"description,omitempty" binding:"omitempty,max=1000" example:"Tài liệu về dependency injection trong Go"`
	CategoryID  *int64  `json:"category_id,omitempty" binding:"omitempty,gt=0" example:"2"`
}

// ListArticlesQuery hứng query ?category_id=&category_name=&limit=&offset=
// bằng tag `form` của gin — lọc theo đúng 1 trong category_id hoặc
// category_name, luật "đúng 1" do service bắt (cross-field).
type ListArticlesQuery struct {
	CategoryID   int64  `form:"category_id" binding:"omitempty,gt=0"`
	CategoryName string `form:"category_name" binding:"omitempty,max=100"`
	Limit        int    `form:"limit" binding:"omitempty,gte=0"`
	Offset       int    `form:"offset" binding:"omitempty,gte=0"`
}

// ArticleResponse trả về cho client — không bao giờ lộ field nội bộ.
type ArticleResponse struct {
	ID          int64  `json:"id" example:"1"`
	UserID      int64  `json:"user_id" example:"7"`
	URL         string `json:"url,omitempty" example:"https://example.com/docs/go-fx"`
	StorageKey  string `json:"storage_key,omitempty" example:"uploads/550e8400-e29b-41d4-a716-446655440000.pdf"`
	Name        string `json:"name" example:"Giới thiệu Go Fx"`
	ContentType string `json:"content_type" example:"text/html"`
	DocType     string `json:"doc_type,omitempty" example:"laws"`
	Description string `json:"description,omitempty" example:"Tài liệu về dependency injection trong Go"`
	CategoryID  int64  `json:"category_id" example:"1"`
	// Trạng thái pipeline (chỉ đọc) để client hiện tiến trình extract → index
	// → compile; thiếu chúng thì UI không biết bài đang xử lý hay đã xong.
	ExtractStatus string    `json:"extract_status" example:"done"`
	IndexStatus   string    `json:"index_status" example:"processing"`
	CompileStatus string    `json:"compile_status" example:"pending"`
	CreatedAt     time.Time `json:"created_at" example:"2026-10-06T10:00:00+07:00"`
	UpdatedAt     time.Time `json:"updated_at" example:"2026-10-06T10:00:00+07:00"`
}

// ArticleListResponse bọc danh sách + tổng số.
type ArticleListResponse struct {
	Articles []*ArticleResponse `json:"articles"`
	Total    int64              `json:"total" example:"10"`
}

// ArticleDownloadResponse là URL GET tạm (đã ký) để client tải file gốc thẳng từ
// kho. filename là tên browser sẽ đặt cho file khi lưu (đã được phía kho nhét
// vào Content-Disposition), gửi kèm để client tự đặt tên cho anchor download
// thay vì suy ra từ key "uploads/<uuid>.pdf" vô nghĩa. content_type chỉ để
// client hiển thị/đoán, không ảnh hưởng bytes tải về.
type ArticleDownloadResponse struct {
	URL         string    `json:"url" example:"http://localhost:8333/app-uploads/uploads/550e8400-e29b-41d4-a716-446655440000.pdf?X-Amz-Signature=…"`
	ExpiresAt   time.Time `json:"expires_at" example:"2026-10-26T15:05:00+07:00"`
	Filename    string    `json:"filename" example:"Quy định nội bộ.pdf"`
	ContentType string    `json:"content_type" example:"application/pdf"`
}

// ToModel map request -> domain model — UserID do service gán từ token,
// không tin client. Nguồn (URL hay StorageKey) service đã chốt đúng 1
// trước khi tới đây.
func (r *CreateArticleRequest) ToModel() *domain.Article {
	return &domain.Article{
		URL:         r.URL,
		StorageKey:  r.StorageKey,
		Name:        r.Name,
		ContentType: r.ContentType,
		DocType:     r.DocType,
		Description: r.Description,
		CategoryID:  r.CategoryID,
	}
}

// ToArticleResponse map domain -> response.
func ToArticleResponse(a *domain.Article) *ArticleResponse {
	if a == nil {
		return &ArticleResponse{}
	}
	return &ArticleResponse{
		ID:          a.ID,
		UserID:      a.UserID,
		URL:         a.URL,
		StorageKey:  a.StorageKey,
		Name:        a.Name,
		ContentType: a.ContentType,
		DocType:     a.DocType,
		Description: a.Description,
		CategoryID:  a.CategoryID,

		ExtractStatus: a.ExtractStatus,
		IndexStatus:   a.IndexStatus,
		CompileStatus: a.CompileStatus,

		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
	}
}

// ToArticleListResponse map danh sách domain -> response.
func ToArticleListResponse(articles []*domain.Article) ArticleListResponse {
	res := ArticleListResponse{
		Articles: make([]*ArticleResponse, 0, len(articles)),
		Total:    int64(len(articles)),
	}
	for _, a := range articles {
		res.Articles = append(res.Articles, ToArticleResponse(a))
	}
	return res
}

// ArticleStatusResponse chỉ gồm id + trạng thái 3 phase — client poll tiến
// trình (khi danh sách còn bài đang xử lý) nên trả metadata là thừa.
type ArticleStatusResponse struct {
	ID            int64  `json:"id" example:"8"`
	ExtractStatus string `json:"extract_status" example:"done"`
	IndexStatus   string `json:"index_status" example:"processing"`
	CompileStatus string `json:"compile_status" example:"pending"`
}

// WorkerStatusResponse tình trạng tiến trình worker tại thời điểm gọi:
// online=false nghĩa là không có heartbeat nào trong WORKER_OFFLINE_AFTER —
// bài "chờ xử lý" sẽ không tiến triển cho tới khi bật lại worker.
type WorkerStatusResponse struct {
	Online     bool       `json:"online" example:"true"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty" example:"2026-10-24T22:30:00+07:00"`
}

// ArticleStatusListResponse bọc danh sách trạng thái + nhịp sống worker —
// UI cần cả hai để biết "đang xử lý" hay "đang chờ vì worker đã tắt".
type ArticleStatusListResponse struct {
	Items  []*ArticleStatusResponse `json:"items"`
	Worker WorkerStatusResponse     `json:"worker"`
}

// ToArticleStatusListResponse map domain -> response trạng thái gọn.
func ToArticleStatusListResponse(articles []*domain.Article) ArticleStatusListResponse {
	res := ArticleStatusListResponse{Items: make([]*ArticleStatusResponse, 0, len(articles))}
	for _, a := range articles {
		if a == nil {
			continue
		}
		res.Items = append(res.Items, &ArticleStatusResponse{
			ID:            a.ID,
			ExtractStatus: a.ExtractStatus,
			IndexStatus:   a.IndexStatus,
			CompileStatus: a.CompileStatus,
		})
	}
	return res
}

// SearchRequest hỏi tri thức — query bắt buộc, category_ids để khoanh nhiều
// chủ đề, limit/threshold để kiểm soát lượng + chất hit. Owner lấy từ token.
// Model là model LLM cho vòng 1 agentic (phân loại câu hỏi) — rỗng thì default
// lookup, khỏi tốn lượt LLM. QuestionType do caller đã classify ở vòng 1
// (luồng chat) truyền sang để khỏi classify lần 2 — SearchAPI gọi trực tiếp
// thì để trống, search tự classify.
type SearchRequest struct {
	Query          string  `json:"query" binding:"required,min=1,max=2000" example:"Go Fx quản lý vòng đời thế nào"`
	CategoryIDs    []int64 `json:"category_ids,omitempty" binding:"omitempty,max=100,dive,gt=0"`
	Limit          int     `json:"limit,omitempty" binding:"omitempty,gte=0"`
	ScoreThreshold float32 `json:"score_threshold,omitempty" binding:"omitempty,gte=0"`
	Model          string  `json:"model,omitempty" example:"gemma4:31b"`
	QuestionType   string  `json:"question_type,omitempty" example:"compare"`
}

// SearchHitResponse là 1 đoạn trúng — agent RAG lấy text làm ngữ cảnh,
// page_num/chunk_index để trích dẫn, score để cân nhắc độ tin.
type SearchHitResponse struct {
	ArticleID  int64   `json:"article_id" example:"8"`
	ChunkIndex int     `json:"chunk_index" example:"11"`
	PageNum    int     `json:"page_num" example:"0"`
	Section    string  `json:"section" example:"ĐIỀU 5: CHẾ ĐỘ ĐÓNG BẢO HIỂM"`
	Score      float32 `json:"score" example:"0.83"`
	Text       string  `json:"text" example:"Chất lượng là ưu tiên hàng đầu..."`
}

// SearchResponse bọc danh sách hit + tổng số + loại câu hỏi vòng 1 (để agent
// biết đường tổng hợp tiếp).
type SearchResponse struct {
	Hits         []*SearchHitResponse `json:"hits"`
	Total        int                  `json:"total" example:"3"`
	QuestionType string               `json:"question_type" example:"compare"`
}

// ToSearchHitResponse map domain -> response.
func ToSearchHitResponse(h *domain.ScoredChunk) *SearchHitResponse {
	if h == nil {
		return &SearchHitResponse{}
	}
	return &SearchHitResponse{
		ArticleID:  h.ArticleID,
		ChunkIndex: h.ChunkIndex,
		PageNum:    h.PageNum,
		Section:    h.Section,
		Score:      h.Score,
		Text:       h.Text,
	}
}

// ArticleSectionsResponse là PageIndex của 1 bài (P2.4 phase 1a) — danh mục
// các mục (Điều/chương/phần) theo thứ tự đọc kèm tọa độ chunk bắt đầu,
// promote từ Qdrant ListSections (tool list_sections của agent đã dùng nòng
// này). Index_status kèm để client phân biệt "bài không có mục nào" với
// "chưa index xong nên mục còn trống". Compile_status (phase 1b) cho biết
// LLM đã tóm tắt xong chưa; mục chưa compile thì summary rỗng.
type ArticleSectionsResponse struct {
	ArticleID     int64                  `json:"article_id" example:"8"`
	ArticleName   string                 `json:"article_name" example:"Quy định nội bộ"`
	IndexStatus   string                 `json:"index_status" example:"done"`
	CompileStatus string                 `json:"compile_status" example:"done"`
	SectionCount  int                    `json:"section_count" example:"12"`
	Sections      []*SectionSpanResponse `json:"sections"`
}

// SectionSpanResponse là 1 mục + chunk bắt đầu — from là tọa độ để đọc nội
// dung vùng đó (scroll range), không phải số trang. Summary do worker compile
// sinh (phase 1b), rỗng khi bài chưa compile.
type SectionSpanResponse struct {
	Name    string `json:"name" example:"ĐIỀU 5: CHẾ ĐỘ ĐÓNG BẢO HIỂM"`
	From    int    `json:"from" example:"40"`
	Summary string `json:"summary" example:"Quy định mức và thời hạn đóng bảo hiểm xã hội..."`
}

// ToArticleSectionsResponse map domain -> response, đảm bảo sections là
// mảng rỗng chứ không null khi bài chưa có mục nào. summaries (name -> LLM
// summary, đọc từ article_sections) được merge theo tên mục — mục chưa compile
// thì summary rỗng, PageIndex bên Qdrant vẫn là nguồn thứ tự + tọa độ chuẩn.
func ToArticleSectionsResponse(a *domain.Article, sections []domain.SectionSpan, summaries map[string]string) *ArticleSectionsResponse {
	res := &ArticleSectionsResponse{
		Sections: make([]*SectionSpanResponse, 0, len(sections)),
	}
	if a != nil {
		res.ArticleID = a.ID
		res.ArticleName = a.Name
		res.IndexStatus = a.IndexStatus
		res.CompileStatus = a.CompileStatus
	}
	for _, s := range sections {
		res.Sections = append(res.Sections, &SectionSpanResponse{
			Name:    s.Name,
			From:    s.From,
			Summary: summaries[s.Name],
		})
	}
	res.SectionCount = len(res.Sections)
	return res
}

// ArticleTreeResponse là cây mục của 1 bài (P2.4 phase 1c) — cùng nguồn dữ
// liệu với PageIndex (Qdrant ListSections + summary đã compile) nhưng nest
// theo phân cấp heading: Chương/Phần > Mục > Điều/Bài. Tài liệu không có
// heading chương thì mọi mục là root (cây phẳng) — không cần LLM, đúng ý
// proposal §4.2 "group sections theo heading hierarchy".
type ArticleTreeResponse struct {
	ArticleID     int64                      `json:"article_id" example:"8"`
	ArticleName   string                     `json:"article_name" example:"Quy định nội bộ"`
	IndexStatus   string                     `json:"index_status" example:"done"`
	CompileStatus string                     `json:"compile_status" example:"done"`
	NodeCount     int                        `json:"node_count" example:"24"`
	Roots         []*SectionTreeNodeResponse `json:"roots"`
}

// SectionTreeNodeResponse là 1 nút cây — tọa độ chunk bắt đầu + summary
// (rỗng khi chưa compile) + con.
type SectionTreeNodeResponse struct {
	Name     string                     `json:"name" example:"CHƯƠNG II: CHẾ ĐỘ NGHỈ PÉP"`
	From     int                        `json:"from" example:"30"`
	Summary  string                     `json:"summary" example:"Quy định 12 ngày phép năm..."`
	Children []*SectionTreeNodeResponse `json:"children"`
}

// treeHeadwordRe nhận diện headword đầu tên mục — cùng bộ heading words
// chunker.SplitBlocks tách được (CHƯƠNG/PHẦN/MỤC/ĐIỀU/BÀI), kể cả kiểu
// in hoa lẫn in thường pha chữ thường phía sau.
var treeHeadwordRe = regexp.MustCompile(`(?i)^\s*(\p{L}+)`)

// sectionLevel gán mức theo từ đầu tên mục: chương/phần = 1, mục = 2,
// điều/bài = 3; tên không thuộc bộ nào (vd tiêu đề tự do) coi là lá (3) để
// treo dưới cha gần nhất thay vì cắt đôi cây.
func sectionLevel(name string) int {
	m := treeHeadwordRe.FindStringSubmatch(name)
	if m == nil {
		return 3
	}
	switch strings.ToUpper(m[1]) {
	case "CHƯƠNG", "PHẦN":
		return 1
	case "MỤC":
		return 2
	default:
		return 3
	}
}

// BuildArticleTree dựng cây từ PageIndex đã sắp xếp (ListSections trả theo
// from tăng dần) bằng stack mức: node treo dưới tổ tiên gần nhất có mức nhỏ
// hơn, không có thì thành root. Đổi cấu trúc heading chỉ cần sửa sectionLevel.
func BuildArticleTree(a *domain.Article, sections []domain.SectionSpan, summaries map[string]string) *ArticleTreeResponse {
	res := &ArticleTreeResponse{Roots: make([]*SectionTreeNodeResponse, 0, len(sections))}
	if a != nil {
		res.ArticleID = a.ID
		res.ArticleName = a.Name
		res.IndexStatus = a.IndexStatus
		res.CompileStatus = a.CompileStatus
	}
	type stackItem struct {
		level int
		node  *SectionTreeNodeResponse
	}
	var stack []stackItem
	for _, s := range sections {
		level := sectionLevel(s.Name)
		node := &SectionTreeNodeResponse{
			Name:     s.Name,
			From:     s.From,
			Summary:  summaries[s.Name],
			Children: make([]*SectionTreeNodeResponse, 0),
		}
		for len(stack) > 0 && stack[len(stack)-1].level >= level {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			res.Roots = append(res.Roots, node)
		} else {
			parent := stack[len(stack)-1].node
			parent.Children = append(parent.Children, node)
		}
		stack = append(stack, stackItem{level: level, node: node})
		res.NodeCount++
	}
	return res
}

// ResetScopeRequest chọn phạm vi bài cho các thao tác admin theo lô (reset
// hàng đợi, trích lại). Scope bắt buộc đúng 1 trong 2: article_ids hoặc
// category_id — cả 2 rỗng service trả 400 để không reset nhầm toàn bộ. Chỉ tác
// động bài của chính mình (owner lấy từ token).
type ResetScopeRequest struct {
	ArticleIDs []int64 `json:"article_ids,omitempty" binding:"omitempty,dive,gt=0"`
	CategoryID int64   `json:"category_id,omitempty" binding:"omitempty,gt=0" example:"5"`
}

// ResetScopeResponse báo số bài đã đưa về pending + danh sách id — worker hốt
// dần theo nhịp claim, theo dõi qua GET /admin/queue như bình thường.
type ResetScopeResponse struct {
	ResetArticles int64   `json:"reset_articles" example:"2"`
	ArticleIDs    []int64 `json:"article_ids"`
}

// RebuildVectorsRequest đưa bài về hàng đợi index từ đầu (Qdrant chết/mất
// collection, chuyển cụm mới). Cùng hình dạng scope với mọi thao tác admin
// theo lô — alias để swagger mô tả đúng từng endpoint thay vì dùng chung
// một tên chung chung.
type RebuildVectorsRequest = ResetScopeRequest

// RebuildVectorsResponse báo số bài đã đưa về pending.
type RebuildVectorsResponse = ResetScopeResponse
