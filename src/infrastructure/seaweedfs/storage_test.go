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

func (f *fakeConfig) GetRedis() config.RedisConfig { return config.RedisConfig{} }

func TestNewStorageBuildsClient(t *testing.T) {
	cfg := &fakeConfig{s3: config.S3Config{
		Type:      "seaweedfs",
		Endpoint:  "http://seaweedfs-s3:8333",
		AccessKey: "key",
		SecretKey: "secret",
		Bucket:    "app-uploads",
	}}

	st, err := NewStorage(cfg)
	if err != nil {
		t.Fatalf("NewStorage returned error: %v", err)
	}
	if st == nil {
		t.Fatal("NewStorage returned nil storage")
	}
	if _, ok := st.(*Storage); !ok {
		t.Fatalf("expected *Storage, got %T", st)
	}
}

func TestNewStorageRejectsUnknownType(t *testing.T) {
	cfg := &fakeConfig{s3: config.S3Config{Type: "s3", Endpoint: "http://x", Bucket: "b"}}
	if _, err := NewStorage(cfg); err == nil {
		t.Fatal("expected error for unsupported storage type")
	}
}

var _ storage.IStorage = (*Storage)(nil)
