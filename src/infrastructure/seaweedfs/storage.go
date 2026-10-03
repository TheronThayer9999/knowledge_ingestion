package seaweedfs

import (
	"context"
	"fmt"
	"io"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/storage"
	"knowledge_ingestion/src/config"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const storageType = "seaweedfs"

type Storage struct {
	client *s3.Client
	bucket string
}

func NewStorage(cfg config.IConfig) (storage.IStorage, error) {
	s := cfg.GetStorage()
	if s.Type != storageType {
		return nil, fmt.Errorf("unsupported storage type %q, expected %q", s.Type, storageType)
	}
	if s.Endpoint == "" {
		return nil, fmt.Errorf("storage endpoint is required")
	}
	if s.Bucket == "" {
		return nil, fmt.Errorf("storage bucket_name is required")
	}

	awsCfg := aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider(s.AccessKey, s.SecretKey, ""),
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(s.Endpoint)
		o.UsePathStyle = true
	})

	logs.Infow("seaweedfs storage ready", "endpoint", s.Endpoint, "bucket", s.Bucket)

	return &Storage{client: client, bucket: s.Bucket}, nil
}

func (s *Storage) Save(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	input := &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          reader,
		ContentLength: aws.Int64(size),
	}
	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}
	if _, err := s.client.PutObject(ctx, input); err != nil {
		return fmt.Errorf("seaweedfs save %s: %w", key, err)
	}
	return nil
}

func (s *Storage) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("seaweedfs open %s: %w", key, err)
	}
	return out.Body, nil
}

func (s *Storage) Delete(ctx context.Context, key string) error {
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}); err != nil {
		return fmt.Errorf("seaweedfs delete %s: %w", key, err)
	}
	return nil
}
