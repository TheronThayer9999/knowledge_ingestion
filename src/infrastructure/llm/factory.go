package llm

import (
	"fmt"
	"strings"

	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"
	"knowledge_ingestion/src/infrastructure/llm/ollama"
)

const (
	ProviderOllama = "ollama"
)

// NewLLM dựng client chat theo provider — cùng pattern factory của embedding.
func NewLLM(cfg config.IConfig) (domain.ILLM, error) {
	lc := cfg.GetLLM()
	switch strings.ToLower(lc.Provider) {
	case ProviderOllama:
		return ollama.New(lc)
	default:
		return nil, fmt.Errorf("unknown llm provider: %q", lc.Provider)
	}
}
