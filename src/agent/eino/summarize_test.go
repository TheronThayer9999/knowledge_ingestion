package eino

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Summary phải giữ nguyên phần đuôi sau user message cuối (tool result của
// lượt hiện tại) — nếu mất thì model bị tóm mất căn cứ [^n] giữa vòng lặp.
func TestKeepRunTailFinalize(t *testing.T) {
	ctx := context.Background()
	sys := schema.SystemMessage("prompt")
	oldUser := schema.UserMessage("câu hỏi lượt 1")
	oldAsst := schema.AssistantMessage("trả lời lượt 1", nil)
	query := schema.UserMessage("câu hỏi lượt 2")
	toolCall := &schema.ToolCall{ID: "c1", Function: schema.FunctionCall{Name: "knowledge_search", Arguments: `{}`}}
	toolMsg := schema.AssistantMessage("", []schema.ToolCall{*toolCall})
	toolResult := schema.ToolMessage("kết quả search", "c1")
	summary := schema.AssistantMessage("SUMMARY", nil)

	out, err := keepRunTailFinalize(ctx, []*schema.Message{sys, oldUser, oldAsst, query, toolMsg, toolResult}, summary)
	require.NoError(t, err)

	require.Len(t, out, 4)
	// DefaultFinalize trả system + summary
	assert.Equal(t, schema.System, out[0].Role)
	assert.Equal(t, "prompt", out[0].Content)
	assert.Contains(t, out[1].Content, "SUMMARY")
	// Đuôi = tool call + tool result của lượt hiện tại, nguyên vẹn
	assert.Equal(t, schema.Assistant, out[2].Role)
	require.Len(t, out[2].ToolCalls, 1)
	assert.Equal(t, "knowledge_search", out[2].ToolCalls[0].Function.Name)
	assert.Equal(t, schema.Tool, out[3].Role)
	assert.Equal(t, "kết quả search", out[3].Content)
}

// Chưa có tool nào chạy (lượt mới tinh) thì chỉ trả system + summary, không
// thêm message thừa phía sau.
func TestKeepRunTailFinalizeNoTail(t *testing.T) {
	ctx := context.Background()
	sys := schema.SystemMessage("prompt")
	query := schema.UserMessage("câu hỏi")
	summary := schema.AssistantMessage("SUMMARY", nil)

	out, err := keepRunTailFinalize(ctx, []*schema.Message{sys, query}, summary)
	require.NoError(t, err)
	require.Len(t, out, 2)
	// DefaultFinalize bọc summary bằng preamble + continuation instruction
	assert.Contains(t, out[1].Content, "SUMMARY")
}

// Summary rỗng (model free thi thoảng trả content "") phải trả lại nguyên gốc
// — không fail cả lượt chat.
func TestKeepRunTailFinalizeEmptySummary(t *testing.T) {
	ctx := context.Background()
	sys := schema.SystemMessage("prompt")
	query := schema.UserMessage("câu hỏi")
	emptySummary := schema.AssistantMessage("", nil)

	out, err := keepRunTailFinalize(ctx, []*schema.Message{sys, query}, emptySummary)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, "câu hỏi", out[1].Content)
}
