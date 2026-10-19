package graphtool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// Compilable là mọi thứ compose biên dịch được thành Runnable — Chain, Graph
// đều thỏa nên tool bọc được cả pipeline 1 lambda lẫn graph nhiều node.
type Compilable[I, O any] interface {
	Compile(ctx context.Context, opts ...compose.GraphCompileOption) (compose.Runnable[I, O], error)
}

// InvokableGraphTool bọc 1 chain/graph thành tool chạy được trong ToolsNode —
// args JSON của LLM unmarshal thành I (schema sinh từ struct I qua
// GoStruct2ToolInfo, cùng cơ chế với InferTool), Invoke rồi marshal O về string.
// Compile chạy mỗi lần gọi tool (overhead nhỏ so với 1 lượt LLM).
type InvokableGraphTool[I, O any] struct {
	compilable     Compilable[I, O]
	compileOptions []compose.GraphCompileOption
	tInfo          *schema.ToolInfo
}

// NewInvokableGraphTool bọc chain/graph đã dựng sẵn thành InvokableTool —
// thêm tool mới = dựng chain + gọi 1 dòng này, không học thêm DSL.
func NewInvokableGraphTool[I, O any](compilable Compilable[I, O], name, desc string, opts ...compose.GraphCompileOption) (tool.InvokableTool, error) {
	if compilable == nil {
		return nil, fmt.Errorf("graphtool %s: chain/graph is required", name)
	}
	info, err := toolutils.GoStruct2ToolInfo[I](name, desc)
	if err != nil {
		return nil, fmt.Errorf("graphtool %s: dựng tool schema: %w", name, err)
	}
	return &InvokableGraphTool[I, O]{compilable: compilable, compileOptions: opts, tInfo: info}, nil
}

func (t *InvokableGraphTool[I, O]) Info(_ context.Context) (*schema.ToolInfo, error) {
	return t.tInfo, nil
}

func (t *InvokableGraphTool[I, O]) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	if argumentsInJSON == "" {
		argumentsInJSON = "{}"
	}
	var in I
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("graphtool %s: decode args: %w", t.tInfo.Name, err)
	}
	runnable, err := t.compilable.Compile(ctx, t.compileOptions...)
	if err != nil {
		return "", fmt.Errorf("graphtool %s: compile: %w", t.tInfo.Name, err)
	}
	out, err := runnable.Invoke(ctx, in)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("graphtool %s: encode output: %w", t.tInfo.Name, err)
	}
	return string(raw), nil
}
