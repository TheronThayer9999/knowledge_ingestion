package eino

import (
	"context"
	stderrors "errors"
	"fmt"
	"sort"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"

	"knowledge_ingestion/src/domain"
)

// ReadContextName là tên tool duy nhất LLM thấy.
const ReadContextName = "read_context"

// maxContextWindow chặn số chunk đọc thêm mỗi bên — agent tò mò đọc cả bài
// thì tốn token mà cites cũng không hiện hết (trần maxCollectedHits).
const maxContextWindow = 3

// ReadContextArgs là args LLM được phép đưa — article_id + chunk_index lấy
// từ hit knowledge_search đã có, window là số chunk đọc thêm mỗi bên.
type ReadContextArgs struct {
	ArticleID  flexInt64 `json:"article_id" jsonschema:"description=ID bài viết lấy từ kết quả knowledge_search (article_id của đoạn đang đọc dở). Bắt buộc."`
	ChunkIndex flexInt   `json:"chunk_index" jsonschema:"description=Chỉ số chunk đang đọc dở (chunk_index của đoạn đó). Bắt buộc."`
	Window     flexInt   `json:"window,omitempty" jsonschema:"description=Số chunk đọc thêm mỗi bên (trước/sau). Mặc định 1, tối đa 3."`
}

// readContextHit là 1 chunk kề trả cho LLM — cùng shape với knowledge_search
// để LLM đối chiếu, n khớp HitCollector cho cites.
type readContextHit struct {
	N          int    `json:"n"`
	ArticleID  int64  `json:"article_id"`
	PageNum    int    `json:"page_num"`
	ChunkIndex int    `json:"chunk_index"`
	Section    string `json:"section,omitempty"`
	Text       string `json:"text"`
}

const readContextDesc = `Đọc các chunk kề trước/sau một đoạn trích đã có để lấy đủ ngữ cảnh khi đoạn đó bị cắt giữa chừng (ví dụ đoạn nói về bảo hiểm nhưng không rõ thuộc Điều nào).

KHI NÀO GỌI: kết quả knowledge_search trả đoạn liên quan nhưng thiếu đầu/đuôi — không rõ tên Điều/Khoản, câu bị cắt nửa chừng, hoặc cần kiểm tra đoạn trước/sau nói gì. Chỉ gọi SAU knowledge_search, với article_id + chunk_index lấy từ hit đã có.

KHI NÀO KHÔNG GỌI: chưa search lần nào (gọi knowledge_search trước); muốn tìm nội dung mới (đổi từ khóa search thay vì đọc lan man); chào hỏi, cảm ơn.

CÁCH DÙNG: mỗi chunk khoảng 1000 token nên window 1 (1 chunk trước + 1 sau, tổng khoảng 3 chunk) là đủ cho hầu hết ca cắt giữa chừng hoặc thiếu tên Điều/Khoản. Chỉ tăng window lên 2 khi quy định trải dài hơn, hoặc vẫn thiếu sau khi đọc window 1. Không dùng 3 trừ khi thật sự cần. Kết quả trả các chunk theo đúng thứ tự đọc, trích dẫn bằng [^n] như knowledge_search.`

// NewReadContextTool bọc IVectorStore.ScrollNeighbors thành Eino
// InvokableTool — DB chunk đã trim sau embed nên đọc từ Qdrant payload.
// Phạm vi tin cậy (user, category ép) đọc qua ScopeFrom(ctx), không tin LLM.
func NewReadContextTool(vectors domain.IVectorStore) (einotool.InvokableTool, error) {
	if vectors == nil {
		return nil, fmt.Errorf("eino read_context: vector store is required")
	}
	return toolutils.InferTool(ReadContextName, readContextDesc,
		func(ctx context.Context, in ReadContextArgs) ([]readContextHit, error) {
			scope, ok := ScopeFrom(ctx)
			if !ok {
				return nil, fmt.Errorf("eino read_context: thiếu scope request (user_id) trong ctx")
			}
			if in.ArticleID <= 0 {
				// LLM gửi "None"/tên file thay vì số (model free) — args không
				// hợp lệ, trả rỗng để tự xoay với ID đúng thay vì abort run 500.
				return []readContextHit{}, nil
			}
			window := int(in.Window)
			if window <= 0 {
				window = 1
			}
			if window > maxContextWindow {
				window = maxContextWindow
			}
			chunks, err := vectors.ScrollNeighbors(ctx, domain.SearchFilter{
				UserID:      scope.UserID,
				CategoryIDs: scope.CategoryIDs,
				ArticleID:   int64(in.ArticleID),
			}, int(in.ChunkIndex), window)
			if err != nil {
				return nil, err
			}
			if scope.Collector != nil {
				scope.Collector.Searched = true
			}
			if len(chunks) == 0 {
				// Hết chunk quanh điểm hỏi (ID bịa, chunk_index quá đà) — trả
				// rỗng để LLM tự xoay (đổi ID, search lại) thay vì error làm
				// ToolNode abort cả run thành 500. Lỗi wiring mới trả error.
				return []readContextHit{}, nil
			}
			// Sort theo ChunkIndex tại tool để thứ tự đọc đúng dù store trả
			// lộn xộn — LLM đọc lan man là trích sai ngữ cảnh.
			sort.Slice(chunks, func(i, j int) bool { return chunks[i].ChunkIndex < chunks[j].ChunkIndex })
			var ns []int
			if scope.Collector != nil {
				ns = scope.Collector.Add(chunks)
			} else {
				ns = make([]int, len(chunks))
				for i := range chunks {
					ns[i] = i + 1
				}
			}
			out := make([]readContextHit, 0, len(chunks))
			for i, h := range chunks {
				if h == nil || ns[i] <= 0 {
					continue
				}
				out = append(out, readContextHit{
					N:          ns[i],
					ArticleID:  h.ArticleID,
					PageNum:    h.PageNum,
					ChunkIndex: h.ChunkIndex,
					Section:    h.Section,
					Text:       h.Text,
				})
			}
			return out, nil
		})
}

// articleInfoItem là metadata 1 bài trả cho LLM — số liệu do worker trích
// lúc chunk (số trang, phiên bản, hiệu lực), không phải LLM tự đếm.
type articleInfoItem struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	ContentType   string `json:"content_type,omitempty"`
	Description   string `json:"description,omitempty"`
	PageCount     int    `json:"page_count"`
	Version       string `json:"version,omitempty"`
	EffectiveDate string `json:"effective_date,omitempty"`
}

// ArticleInfoName là tên tool duy nhất LLM thấy.
const ArticleInfoName = "article_info"

// ArticleInfoArgs là args LLM được phép đưa — 1 trong 2: xem 1 bài cụ thể
// hay liệt kê bài trong 1 chủ đề.
type ArticleInfoArgs struct {
	ArticleID  flexInt64 `json:"article_id,omitempty" jsonschema:"description=ID bài viết cụ thể (lấy từ kết quả search). Có thì xem đúng 1 bài."`
	CategoryID flexInt64 `json:"category_id,omitempty" jsonschema:"description=ID chủ đề để liệt kê các bài trong đó. Bỏ trống thì liệt kê mọi chủ đề của bạn."`
}

const articleInfoDesc = `Xem thông tin tài liệu đã upload: tên file, số trang, phiên bản, ngày hiệu lực, mô tả.

KHI NÀO GỌI: người dùng hỏi về bản thân tài liệu ("file này có bao nhiêu trang", "phiên bản là gì", "hiệu lực từ khi nào", "file đó là gì") hoặc cần đối chiếu tên file với ID trước khi đọc sâu. Số trang/phiên bản do hệ thống trích lúc xử lý file — chỉ dùng số tool trả về, không tự đếm.

KHI NÀO KHÔNG GỌI: hỏi nội dung bên trong tài liệu (gọi knowledge_search); chào hỏi, cảm ơn.

CÁCH DÙNG: biết ID bài thì truyền article_id; muốn liệt kê thì truyền category_id (bỏ trống là mọi chủ đề). Không có thông tin nào thì nói rõ "tài liệu không ghi", không đoán.`

// NewArticleInfoTool bọc ArticleRepository thành Eino InvokableTool — chỉ
// thấy bài của đúng owner trong Scope, LLM không có đường đọc ké bài user
// khác (GetByID/ListByCategoryID đều khóa user).
func NewArticleInfoTool(repo domain.IArticleRepository) (einotool.InvokableTool, error) {
	if repo == nil {
		return nil, fmt.Errorf("eino article_info: article repo is required")
	}
	return toolutils.InferTool(ArticleInfoName, articleInfoDesc,
		func(ctx context.Context, in ArticleInfoArgs) ([]articleInfoItem, error) {
			scope, ok := ScopeFrom(ctx)
			if !ok {
				return nil, fmt.Errorf("eino article_info: thiếu scope request (user_id) trong ctx")
			}
			toItem := func(a *domain.Article) articleInfoItem {
				return articleInfoItem{
					ID: a.ID, Name: a.Name, ContentType: a.ContentType,
					Description: a.Description, PageCount: a.PageCount,
					Version: a.Version, EffectiveDate: a.EffectiveDate,
				}
			}
			if in.ArticleID > 0 {
				a, err := repo.GetByID(ctx, int64(in.ArticleID), scope.UserID)
				// Bài không tồn tại (LLM bịa ID, bài đã xóa) — trả rỗng để LLM
				// tự xoay thay vì error làm ToolNode abort cả run thành 500.
				// Lỗi hạ tầng (DB) vẫn trả error để caller map 500 đúng.
				if stderrors.Is(err, domain.ErrNotFound) || (err == nil && a == nil) {
					return []articleInfoItem{}, nil
				}
				if err != nil {
					return nil, err
				}
				if scope.Collector != nil {
					scope.Collector.Searched = true
				}
				return []articleInfoItem{toItem(a)}, nil
			}
			ids := scope.CategoryIDs
			if in.CategoryID > 0 {
				ids = []int64{int64(in.CategoryID)}
			}
			var out []articleInfoItem
			if len(ids) == 0 {
				// Không rõ chủ đề (request không ép category) — trả rỗng để
				// LLM gọi list_categories rồi hỏi lại, thay vì error abort
				// cả run thành 500.
				return []articleInfoItem{}, nil
			}
			for _, cid := range ids {
				arts, err := repo.ListByCategoryID(ctx, cid, scope.UserID, 50, 0)
				if err != nil {
					return nil, err
				}
				for _, a := range arts {
					if a == nil {
						continue
					}
					out = append(out, toItem(a))
				}
			}
			if scope.Collector != nil {
				scope.Collector.Searched = true
			}
			return out, nil
		})
}
