package storage

import (
	"context"
	"io"
)

type IStorage interface {
	Save(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}
