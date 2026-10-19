package eino

import (
	"context"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"

	"knowledge_ingestion/src/domain"
)

// ListCategoriesName là tên tool duy nhất LLM thấy.
const ListCategoriesName = "list_categories"

// ListCategoriesArgs rỗng có chủ ý — user lấy từ Scope trong ctx (token),
// không cho LLM truyền gì để khỏi bịa phạm vi (theo quy ước ở context.go).
type ListCategoriesArgs struct{}

// listCategoriesItem là 1 chủ đề trả cho LLM — gọn để LLM tự đối chiếu tên
// user nhắc với ID, rồi truyền ID vào knowledge_search.
type listCategoriesItem struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

const listCategoriesDesc = `Liệt kê các chủ đề (category) mà NGƯỜI DÙNG ĐANG HỎI đã tạo để phân loại tài liệu upload.

KHI NÀO GỌI: người dùng nhắc tên chủ đề bằng lời ("trong chủ đề thuế...", "tài liệu bảo hiểm của tôi...") mà câu hỏi cần căn cứ tài liệu; hoặc hỏi chung chung "tôi có những chủ đề nào". Gọi tool này TRƯỚC rồi đối chiếu tên để lấy đúng ID, sau đó truyền ID vào category_ids của knowledge_search.

KHI NÀO KHÔNG GỌI: người dùng đã nêu rõ ID số; chào hỏi, cảm ơn, xưng hô thuần túy không kèm nội dung tri thức. Khi không chắc có dính đến tài liệu hay không, luôn gọi tool tìm kiếm một lần trước rồi mới kết luận.

KẾT QUẢ TRẢ VỀ: JSON gồm các phần tử {id, name, description}. Chỉ dùng ID có trong kết quả, không bịa ID. Không tìm thấy tên nào khớp thì tìm mọi chủ đề (bỏ trống category_ids ở knowledge_search), không đoán.`

// NewListCategoriesTool bọc CategoryRepository thành tool — đi qua chain 1
// lambda + graph-tool (xem chaintool.go) nên cùng họ với graph nhiều node;
// chỉ liệt kê chủ đề của đúng owner trong Scope, LLM không có đường đọc ké
// chủ đề của user khác.
func NewListCategoriesTool(repo domain.ICategoryRepositoryImpl) (einotool.InvokableTool, error) {
	if repo == nil {
		return nil, fmt.Errorf("eino list_categories: category repo is required")
	}
	return NewChainTool(ListCategoriesName, listCategoriesDesc,
		func(ctx context.Context, _ ListCategoriesArgs) ([]listCategoriesItem, error) {
			scope, ok := ScopeFrom(ctx)
			if !ok {
				return nil, fmt.Errorf("eino list_categories: thiếu scope request (user_id) trong ctx")
			}
			cats, err := repo.GetAll(ctx, scope.UserID)
			if err != nil {
				return nil, err
			}
			out := make([]listCategoriesItem, 0, len(cats))
			for _, c := range cats {
				if c == nil {
					continue
				}
				out = append(out, listCategoriesItem{
					ID:          c.ID,
					Name:        c.Name,
					Description: c.Description,
				})
			}
			return out, nil
		})
}
