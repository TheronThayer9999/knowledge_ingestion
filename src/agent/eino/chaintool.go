package eino

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"

	"knowledge_ingestion/src/agent/eino/graphtool"
)

// Thêm tool mới cho agent chỉ cần 2 bước, không đụng usecase/prompt/loader:
//  1. Tạo 1 file trong package này: struct Args (field nào LLM được truyền thì
//     khai báo + tag jsonschema mô tả), kiểu Output, 1 const desc, rồi gọi
//     NewChainTool — KHÔNG cần ghi tham số generic, Go tự suy từ func:
//     tool, err := NewChainTool("my_tool", "Description of what this tool does",
//         func(ctx context.Context, in MyInput) (MyOutput, error) {
//             return MyOutput{Result: "Processed: " + in.Query}, nil
//         })
//
//	type MyInput struct {
//	    Query string `json:"query" jsonschema_description:"The query to process"`
//	}
//	type MyOutput struct {
//	    Result string `json:"result"`
//	}
//
//  2. Thêm 1 dòng vào slice trong NewDefaultTools (tools.go, cùng package).
//     Desc + schema args là nguồn thật duy nhất LLM thấy — viết cho LLM đọc,
//     đừng ghi tóm tắt ở prompt file. Phạm vi tin cậy (user, category ép) đọc
//     qua ScopeFrom(ctx), không tin args LLM; kết quả nhiều vòng gom qua
//     scope.Collector để cites khớp [^n].

// NewChainTool dựng tool từ 1 func Go qua chain 1 lambda + bọc graph-tool —
// tương đương InferTool cũ nhưng cùng họ với graph-tool nhiều node nên sau
// này tool phình thành pipeline vẫn giữ nguyên cách đọc và cách đăng ký.
func NewChainTool[I, O any](name, desc string, fn func(ctx context.Context, in I) (O, error)) (tool.InvokableTool, error) {
	chain := compose.NewChain[I, O]()
	chain.AppendLambda(compose.InvokableLambda(fn))
	return graphtool.NewInvokableGraphTool(chain, name, desc)
}
