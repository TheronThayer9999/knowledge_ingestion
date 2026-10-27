package eino

import (
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"

	"knowledge_ingestion/src/agent/retrieval"
	"knowledge_ingestion/src/domain"
)

// ToolDeps gom mọi dependency các tool cần — thêm tool mới dùng đồ có sẵn
// thì không động vào struct này; cần đồ mới (repo/client khác) thì thêm 1
// field ở đây, loader truyền vào 1 chỗ duy nhất.
type ToolDeps struct {
	Hybrid     *retrieval.HybridSearcher
	Vectors    domain.IVectorStore
	Categories domain.ICategoryRepositoryImpl
	Articles   domain.IArticleRepository
	// Pages là vai hẹp "trang nào không đọc được" — tool báo độ phủ của file.
	Pages domain.ArticlePageIssues
	// PageText là vai hẹp "đọc text đã trích theo cửa sổ" — tool sheet của
	// Excel (list_sheets/read_sheet) đọc lại từ article_pages.
	PageText domain.ArticlePageText
	// ChartEnabled đăng ký tool chart_from_cite (Đợt 26) — cờ `agent.chart_enabled`,
	// tắt thì model không thấy tool này và `ChatResponse.charts` luôn rỗng.
	// Đặt ở ToolDeps để loader/agent.go không phải biết danh sách tool.
	ChartEnabled bool
}

// NewDefaultTools dựng toàn bộ tool cho agent theo đúng thứ tự đăng ký —
// thêm tool mới chỉ cần thêm 1 dòng vào slice dưới (cùng package, ngay cạnh
// các tool khác), loader/agent.go không phải sửa nữa.
func NewDefaultTools(d ToolDeps) ([]einotool.BaseTool, error) {
	search, err := NewKnowledgeSearchTool(d.Hybrid, d.Vectors)
	if err != nil {
		return nil, err
	}
	cats, err := NewListCategoriesTool(d.Categories)
	if err != nil {
		return nil, err
	}
	ctxTool, err := NewReadContextTool(d.Vectors)
	if err != nil {
		return nil, err
	}
	info, err := NewArticleInfoTool(d.Articles)
	if err != nil {
		return nil, err
	}
	sections, err := NewListSectionsTool(d.Vectors)
	if err != nil {
		return nil, err
	}
	readArt, err := NewReadArticleTool(d.Vectors)
	if err != nil {
		return nil, err
	}
	issues, err := NewListUnreadablePagesTool(d.Articles, d.Pages)
	if err != nil {
		return nil, err
	}
	sheetTools, err := newSheetAndChartTools(d)
	if err != nil {
		return nil, err
	}
	tools := append([]einotool.BaseTool{search, cats, ctxTool, info, sections, readArt, issues}, sheetTools...)
	for _, t := range tools {
		if t == nil {
			return nil, fmt.Errorf("eino tools: tool nil")
		}
	}
	return tools, nil
}

// newSheetAndChartTools dựng nhóm tool đọc bảng tính Excel + tool vẽ biểu đồ —
// tách khỏi NewDefaultTools vì thêm cờ chart_enabled là đủ vượt ngân sách điểm
// quyết định (src/quality/function_budget_test.go), không phải vì tái sử dụng.
func newSheetAndChartTools(d ToolDeps) ([]einotool.BaseTool, error) {
	sheets, err := NewListSheetsTool(d.Articles, d.PageText)
	if err != nil {
		return nil, err
	}
	sheet, err := NewReadSheetTool(d.Articles, d.PageText)
	if err != nil {
		return nil, err
	}
	out := []einotool.BaseTool{sheets, sheet}
	if d.ChartEnabled {
		// Tool vẽ biểu đồ: model chỉ chọn nguồn/loại/tiêu đề, số do backend cắt
		// từ chunk đã dẫn — tắt được bằng `agent.chart_enabled` mà không revert.
		chart, err := NewChartFromCiteTool()
		if err != nil {
			return nil, err
		}
		out = append(out, chart)
	}
	return out, nil
}
