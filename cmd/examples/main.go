package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"knowledge_ingestion/cmd/examples/config"
	"log"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"
)

func float32Ptr(f float32) *float32 { return &f }

// ToolRegistry quản lý tất cả tools
type ToolRegistry struct {
	tools     []*schema.ToolInfo
	executors map[string]func(args map[string]string) (string, error)
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools:     make([]*schema.ToolInfo, 0),
		executors: make(map[string]func(args map[string]string) (string, error)),
	}
}

// RegisterTool đăng ký tool và handler cùng lúc
func (r *ToolRegistry) RegisterTool(tool *schema.ToolInfo, handler func(args map[string]string) (string, error)) {
	r.tools = append(r.tools, tool)
	r.executors[tool.Name] = handler
}

// GetTools trả về danh sách tools để bind
func (r *ToolRegistry) GetTools() []*schema.ToolInfo {
	return r.tools
}

// Execute thực thi tool theo tên
func (r *ToolRegistry) Execute(name string, args map[string]string) (string, error) {
	executor, ok := r.executors[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	return executor(args)
}

func main() {
	config.InitFlags()
	flag.Parse()

	// ✅ KHỞI TẠO REGISTRY
	registry := NewToolRegistry()

	// ✅ ĐĂNG KÝ TOOL + HANDLER CÙNG LÚC
	registry.RegisterTool(&schema.ToolInfo{
		Name: "read_project_file",
		Desc: "Đọc NỘI DUNG của một file văn bản nội bộ dự án.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"filename": {
				Type: schema.String,
				Desc: "Tên file chính xác cần đọc",
				Enum: []string{"rag_test_data.txt", "hr_policy_data.txt", "incident_ops_guide.txt"},
			},
		}),
	}, func(args map[string]string) (string, error) {
		content, err := config.ReadDataFromFile(args["filename"])
		if err != nil {
			return "", fmt.Errorf("read file %s: %w", args["filename"], err)
		}
		return content, nil
	})

	registry.RegisterTool(&schema.ToolInfo{
		Name:        "list_data_files",
		Desc:        "Liệt kê danh sách hoặc đếm số lượng file trong thư mục data của dự án.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}, func(args map[string]string) (string, error) {
		files, err := config.GetListFileInStore()
		if err != nil {
			return "", fmt.Errorf("list files: %w", err)
		}
		return fmt.Sprintf("Có %d file: %v", len(files), files), nil
	})

	registry.RegisterTool(&schema.ToolInfo{
		Name: "write_project_file",
		Desc: "Thực hiện để ghi thông tin vào một file văn bản nội bộ dự án.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"filename": {
				Type: schema.String,
				Desc: "Tên file chính xác cần ghi",
			},
			"content": {
				Type: schema.String,
				Desc: "Nội dung cần ghi vào file",
			},
		}),
	}, func(args map[string]string) (string, error) {
		err := config.WriteDataToFile(args["filename"], args["content"])
		if err != nil {
			return "", fmt.Errorf("write file %s: %w", args["filename"], err)
		}
		return fmt.Sprintf("Đã ghi nội dung vào file %s", args["filename"]), nil
	})

	query := "Hệ thống ECS quy định sử dụng phiên bản .NET nào và dùng ORM gì? và sau đó ghi lại câu trả lời vào file cho tôi"
	answer, err := runAgent(query, registry)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("\n=== ANSWER ===")
	fmt.Println(answer)
}

func runAgent(userQuery string, registry *ToolRegistry) (string, error) {
	ctx := context.Background()
	model, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL:     "https://openrouter.ai/api/v1",
		APIKey:      config.OpenRouterKey,
		Model:       "qwen/qwen3-32b",
		Temperature: float32Ptr(0.1),
	})
	if err != nil {
		return "", fmt.Errorf("create model: %w", err)
	}

	// ✅ BIND TẤT CẢ TOOLS TỪ REGISTRY
	if err := model.BindTools(registry.GetTools()); err != nil {
		return "", fmt.Errorf("bind tools: %w", err)
	}

	messages := []*schema.Message{
		schema.SystemMessage(`Bạn là trợ lý AI với 3 chế độ:

CHẾ ĐỘ 1 - TỰ TRẢ LỜI: Kiến thức chung (toán, lập trình cơ bản...). Trả lời ngay.
CHẾ ĐỘ 2 - GỌI TOOL:
  - read_project_file: Khi user cần ĐỌC NỘI DUNG file cụ thể
  - list_data_files: Khi user hỏi SỐ LƯỢNG hoặc DANH SÁCH file trong thư mục data
  - write_project_file: Khi user cần GHI thông tin vào file cụ thể

QUY TẮC: Hỏi về số lượng/danh sách → list_data_files. Hỏi về nội dung → read_project_file. Hỏi về ghi file → write_project_file.`),
		schema.UserMessage(userQuery),
	}

	for i := range 5 {
		resp, err := model.Generate(ctx, messages)
		if err != nil {
			return "", fmt.Errorf("generate: %w", err)
		}

		log.Printf("[Round %d] ToolCalls: %d | Content: %.80s...",
			i+1, len(resp.ToolCalls), resp.Content)

		if len(resp.ToolCalls) == 0 {
			return resp.Content, nil
		}

		messages = append(messages, resp)

		for _, tc := range resp.ToolCalls {
			var args map[string]string
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				return "", fmt.Errorf("parse args: %w", err)
			}

			// ✅ EXECUTE QUA REGISTRY
			result, execErr := registry.Execute(tc.Function.Name, args)
			if execErr != nil {
				log.Printf("[TOOL FAIL] %s: %v", tc.Function.Name, execErr)
				result = fmt.Sprintf("LỖI: %v", execErr)
			} else {
				log.Printf("✅ [TOOL OK] %s: %s", tc.Function.Name, result[:min(60, len(result))])
			}

			toolMsg := &schema.Message{
				Role:       schema.Tool,
				Name:       tc.Function.Name,
				Content:    result,
				ToolCallID: tc.ID,
			}
			messages = append(messages, toolMsg)
		}
	}
	return "", fmt.Errorf("agent vượt quá số lần lặp")
}
