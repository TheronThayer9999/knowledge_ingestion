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
	tools := []einotool.BaseTool{search, cats, ctxTool, info, sections, readArt}
	for _, t := range tools {
		if t == nil {
			return nil, fmt.Errorf("eino tools: tool nil")
		}
	}
	return tools, nil
}
