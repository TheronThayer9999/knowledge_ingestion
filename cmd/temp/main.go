package main

import (
	"context"
	"encoding/json"
	"log"
	"os"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"
)

// weatherTool, calculatorTool
func weatherTool() string {
	return "call weatherTool() ok"
}
func calculator() string {
	return "call calculator() ok"
}

// Tools: []tool.BaseTool{weatherTool, calculatorTool},
func main() {
	ctx := context.Background()
	model, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL:     "https://api.groq.com/openai/v1",
		APIKey:      os.Getenv("GROQ_API_KEY"),
		Model:       "openai/gpt-oss-120b",
		Temperature: float32Ptr(0.7),
	})
	if err != nil {
		log.Fatal(err)
	}

	messages := []*schema.Message{
		schema.SystemMessage("You are a helpful assistant"),
		schema.UserMessage("What is go programming language?"),
	}
	resp, err := model.Generate(ctx, messages)
	if err != nil {
		log.Fatal(err.Error())
	}

	// 5. Marshal sang JSON
	jsonData, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	println(jsonData)

}
func float32Ptr(f float32) *float32 {
	return &f
}
