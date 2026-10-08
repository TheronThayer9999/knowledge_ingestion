package domain

import "context"

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Dimension() int
	ModelName() string
	// IsPermanentError báo lỗi có retry cũng vậy không (sai model/dim, 4xx)
	// — service dùng để failed luôn thay vì backoff. Triển khai ở infra.
	IsPermanentError(err error) bool
}
