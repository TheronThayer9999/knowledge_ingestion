package embedding

import (
	"fmt"
	"strings"

	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"
	"knowledge_ingestion/src/infrastructure/embedding/ollama"
)

const (
	ProviderOllama = "ollama"
)

func NewEmbedder(cfg config.IConfig) (domain.Embedder, error) {
	ec := cfg.GetEmbedding()
	switch strings.ToLower(ec.Provider) {
	case ProviderOllama:
		return ollama.New(ec)
	default:
		return nil, fmt.Errorf("unknown embedding provider: %q", ec.Provider)
	}
}
