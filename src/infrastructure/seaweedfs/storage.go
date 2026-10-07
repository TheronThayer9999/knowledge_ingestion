package seaweedfs

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/storage"
	"knowledge_ingestion/src/config"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

const storageType = "seaweedfs"

type Storage struct {
	client *s3.Client
	bucket string
}

func NewStorage(cfg config.IConfig) (storage.IStorage, error) {
	client, bucket, err := buildClient(cfg)
	if err != nil {
		return nil, err
	}
	// Lifetime-init của host S3: ping bucket, chưa có thì tạo — chạy 1 lần
	// lúc fx dựng graph (startup), fail-fast giống postgres để không boot
	// app trong trạng thái upload mù.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ensureBucket(ctx, client, bucket); err != nil {
		return nil, err
	}

	logs.Infow("seaweedfs storage ready", "endpoint", cfg.GetStorage().Endpoint, "bucket", bucket)

	return &Storage{client: client, bucket: bucket}, nil
}

// buildClient dựng s3.Client thuần túy (không I/O) — tách riêng để unit
// test được mà không cần SeaweedFS chạy.
func buildClient(cfg config.IConfig) (*s3.Client, string, error) {
	s := cfg.GetStorage()
	if s.Type != storageType {
		return nil, "", fmt.Errorf("unsupported storage type %q, expected %q", s.Type, storageType)
	}
	if s.Endpoint == "" {
		return nil, "", fmt.Errorf("storage endpoint is required")
	}
	if s.Bucket == "" {
		return nil, "", fmt.Errorf("storage bucket_name is required")
	}

	awsCfg := aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider(s.AccessKey, s.SecretKey, ""),
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(s.Endpoint)
		o.UsePathStyle = true
	})
	return client, s.Bucket, nil
}

// ensureBucket HeadBucket trước — bucket có rồi thì xong; chưa có thì tạo.
// Race 2 instance cùng tạo thì S3 báo AlreadyExists/OwnedByYou, coi như
// thành công. Lỗi mạng/quyền thì trả error để fail-fast lúc startup.
func ensureBucket(ctx context.Context, client *s3.Client, bucket string) error {
	if _, err := client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	}); err == nil {
		return nil
	}
	if _, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	}); err != nil {
		var alreadyExists *types.BucketAlreadyExists
		var ownedByYou *types.BucketAlreadyOwnedByYou
		if stderrors.As(err, &alreadyExists) || stderrors.As(err, &ownedByYou) {
			return nil
		}
		return fmt.Errorf("seaweedfs ensure bucket %s: %w", bucket, err)
	}
	logs.Infow("seaweedfs bucket created", "bucket", bucket)
	return nil
}

// Upload ghi file bằng PutObject tới SeaweedFS S3 gateway (path-style, static creds, region us-east-1).
func (s *Storage) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
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
		return fmt.Errorf("seaweedfs upload %s: %w", key, err)
	}
	return nil
}

// Download lấy object bằng GetObject và trả stream; caller bắt buộc Close().
func (s *Storage) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("seaweedfs download %s: %w", key, err)
	}
	return out.Body, nil
}

// PresignedURL ký URL PUT tạm thời bằng PresignPutObject để client upload
// thẳng vào SeaweedFS. Không có I/O mạng — chỉ tính HMAC cục bộ, secret_key
// không bao giờ rời khỏi đây. Content-Type ký kèm, client bắt buộc gửi đúng.
func (s *Storage) PresignedURL(ctx context.Context, key string, contentType string, expiry time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(s.client)
	res, err := presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, func(o *s3.PresignOptions) {
		o.Expires = expiry
	})
	if err != nil {
		return "", fmt.Errorf("seaweedfs presign %s: %w", key, err)
	}
	return res.URL, nil
}

// Exists hỏi kho bằng HeadObject — object có thì true, S3 báo NotFound thì
// (false, nil), còn lại (mạng/quyền/bucket sai) là error thật.
func (s *Storage) Exists(ctx context.Context, key string) (bool, error) {
	if _, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}); err != nil {
		var apiErr smithy.APIError
		if stderrors.As(err, &apiErr) && apiErr.ErrorCode() == "NotFound" {
			return false, nil
		}
		return false, fmt.Errorf("seaweedfs exists %s: %w", key, err)
	}
	return true, nil
}

// Delete xóa object bằng DeleteObject; S3 trả thành công cả khi key không tồn tại.
func (s *Storage) Delete(ctx context.Context, key string) error {
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}); err != nil {
		return fmt.Errorf("seaweedfs delete %s: %w", key, err)
	}
	return nil
}
