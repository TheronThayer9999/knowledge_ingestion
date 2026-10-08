package seaweedfs

import (
	"knowledge_ingestion/src/common/storage"
	"knowledge_ingestion/src/config"
	"testing"
)

type fakeConfig struct {
	s3 config.S3Config
}

func (f *fakeConfig) GetApp() config.AppConfig     { return config.AppConfig{} }
func (f *fakeConfig) GetDatabase() config.DBConfig { return config.DBConfig{} }
func (f *fakeConfig) GetStorage() config.S3Config  { return f.s3 }

func (f *fakeConfig) GetEmbedding() config.EmbeddingConfig { return config.EmbeddingConfig{} }

func (f *fakeConfig) GetQdrant() config.QdrantConfig { return config.QdrantConfig{} }

func (f *fakeConfig) GetOCR() config.OCRConfig { return config.OCRConfig{} }

func (f *fakeConfig) GetRedis() config.RedisConfig { return config.RedisConfig{} }

func (f *fakeConfig) GetRabbitMQ() config.RabbitMQConfig { return config.RabbitMQConfig{} }

func (f *fakeConfig) GetJWT() config.JWTConfig { return config.JWTConfig{} }

func (f *fakeConfig) GetCORS() config.CorsConfig { return config.CorsConfig{} }

func TestNewStorageBuildsClient(t *testing.T) {
	cfg := &fakeConfig{s3: config.S3Config{
		Type:      "seaweedfs",
		Endpoint:  "http://seaweedfs-s3:8333",
		AccessKey: "key",
		SecretKey: "secret",
		Bucket:    "app-uploads",
	}}

	// NewStorage giờ lifetime-init (HeadBucket/CreateBucket) nên cần S3
	// thật — unit test chỉ cover buildClient thuần túy (không I/O).
	client, bucket, err := buildClient(cfg)
	if err != nil {
		t.Fatalf("buildClient returned error: %v", err)
	}
	if client == nil {
		t.Fatal("buildClient returned nil client")
	}
	if bucket != "app-uploads" {
		t.Fatalf("expected bucket app-uploads, got %q", bucket)
	}
}

func TestNewStorageRejectsUnknownType(t *testing.T) {
	cfg := &fakeConfig{s3: config.S3Config{Type: "s3", Endpoint: "http://x", Bucket: "b"}}
	if _, err := NewStorage(cfg); err == nil {
		t.Fatal("expected error for unsupported storage type")
	}
}

var _ storage.IStorage = (*Storage)(nil)
