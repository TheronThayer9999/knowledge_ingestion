package qdrant

// Test integration với Qdrant thật qua gRPC — cần Qdrant chạy ở
// 127.0.0.1:6334 (docker compose đã mở sẵn). Không chạy thì skip để CI/dev
// không có Qdrant vẫn xanh, giống pattern connection_test của postgres.

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"knowledge_ingestion/src/domain"

	qdrantapi "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// dialTest nối Qdrant thật — không nối được thì skip, không fail.
func dialTest(t *testing.T) *qdrantapi.Client {
	t.Helper()
	host := "127.0.0.1"
	if h := os.Getenv("QDRANT_TEST_HOST"); h != "" {
		host = h
	}
	token := os.Getenv("QDRANT_TOKEN")
	if token == "" {
		token = "theron_qdrant_token" // khớp docker compose local
	}
	client, err := qdrantapi.NewClient(&qdrantapi.Config{
		Host:                   host,
		Port:                   6334,
		APIKey:                 token,
		SkipCompatibilityCheck: true,
		VersionCheckTimeout:    5 * time.Second,
	})
	if err != nil {
		t.Skipf("không tạo được client qdrant (%s:6334): %v", host, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.CollectionExists(ctx, "article_chunks_test_ping"); err != nil {
		t.Skipf("qdrant không chạy (%s:6334): %v", host, err)
	}
	return client
}

// testConn dựng connection trỏ collection riêng từng test + dọn sau khi xong
// để không chạm collection production article_chunks.
func testConn(t *testing.T, client *qdrantapi.Client, collection string, dim int) *connection {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c := &connection{client: client, dim: dim, collection: collection}
	if err := c.ensureCollection(ctx); err != nil {
		t.Fatalf("ensureCollection: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = client.DeleteCollection(ctx, collection)
	})
	return c
}

func testPoint(id string, articleID int64, dim int) *domain.VectorPoint {
	vec := make([]float32, dim)
	for i := range vec {
		vec[i] = 0.1 * float32(i+1)
	}
	return &domain.VectorPoint{
		ID: id, Vector: vec,
		ArticleID: articleID, UserID: 7, CategoryID: 3,
		ChunkIndex: 1, PageNum: 2, Text: "đoạn test",
	}
}

// Upsert → count → đọc payload → xóa → đếm lại: vòng đời đầy đủ 1 bài.
func TestUpsertCountDelete_Integration(t *testing.T) {
	client := dialTest(t)
	c := testConn(t, client, "article_chunks_test_crud", 4)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := c.UpsertPoints(ctx, []*domain.VectorPoint{
		testPoint("550e8400-e29b-41d4-a716-446655440000", 5, 4),
		testPoint("550e8400-e29b-41d4-a716-446655440001", 5, 4),
		testPoint("550e8400-e29b-41d4-a716-446655440002", 6, 4),
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if n, err := c.CountByArticle(ctx, 5); err != nil || n != 2 {
		t.Fatalf("count bài 5 = %d, err = %v, want 2", n, err)
	}
	// Payload đọc lại phải đủ 6 field phân quyền + text.
	withPayload := true
	var scrollLimit uint32 = 10
	got, err := client.Scroll(ctx, &qdrantapi.ScrollPoints{
		CollectionName: "article_chunks_test_crud",
		Filter:         articleFilter(5),
		Limit:          &scrollLimit,
		WithPayload:    &qdrantapi.WithPayloadSelector{SelectorOptions: &qdrantapi.WithPayloadSelector_Enable{Enable: withPayload}},
	})
	if err != nil {
		t.Fatalf("scroll: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("scroll = %d points, want 2", len(got))
	}
	for _, k := range []string{"article_id", "user_id", "category_id", "chunk_index", "page_num", "text"} {
		if _, ok := got[0].Payload[k]; !ok {
			t.Fatalf("payload thiếu %s: %v", k, got[0].Payload)
		}
	}
	if err := c.DeleteByArticle(ctx, 5); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n, err := c.CountByArticle(ctx, 5); err != nil || n != 0 {
		t.Fatalf("count sau xóa = %d, err = %v, want 0", n, err)
	}
	// Bài khác không bị ảnh hưởng.
	if n, err := c.CountByArticle(ctx, 6); err != nil || n != 1 {
		t.Fatalf("count bài 6 = %d, err = %v, want 1", n, err)
	}
}

// Collection có sẵn sai dim → lỗi fail-fast, không upsert mù.
func TestEnsureCollection_WrongDimFails_Integration(t *testing.T) {
	client := dialTest(t)
	testConn(t, client, "article_chunks_test_dim", 4)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	bad := &connection{client: client, dim: 5, collection: "article_chunks_test_dim"}
	if err := bad.ensureCollection(ctx); err == nil {
		t.Fatal("sai dim phải lỗi")
	}
}

// Vector sai dim thì lỗi ngay phía client, không cần server.
func TestUpsertPoints_DimMismatch(t *testing.T) {
	c := &connection{dim: 4}
	err := c.UpsertPoints(context.Background(), []*domain.VectorPoint{
		{ID: "x", Vector: []float32{0.1}},
	})
	if err == nil {
		t.Fatal("sai dim phải lỗi")
	}
	if !IsPermanent(err) {
		t.Fatalf("sai dim phải là lỗi vĩnh viễn, got %v", err)
	}
}

// Upsert rỗng/nil → no-op, không cần server.
func TestUpsertPoints_EmptyNoop(t *testing.T) {
	c := &connection{}
	if err := c.UpsertPoints(context.Background(), nil); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

// Phân loại vĩnh viễn/không qua mã gRPC — không cần server.
func TestIsPermanent(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"bad request", ErrBadRequest, true},
		{"invalid argument", status.Error(codes.InvalidArgument, "sai"), true},
		{"not found", status.Error(codes.NotFound, "mất"), true},
		{"unauthenticated", status.Error(codes.Unauthenticated, "sai key"), true},
		{"unavailable", status.Error(codes.Unavailable, "rớt mạng"), false},
		{"resource exhausted", status.Error(codes.ResourceExhausted, "nghẽn"), false},
		{"deadline", status.Error(codes.DeadlineExceeded, "timeout"), false},
		{"lỗi thường", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		if got := IsPermanent(tc.err); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
		// Method trên connection phải cùng kết quả với hàm thuần.
		if got := (&connection{}).IsPermanentError(tc.err); got != tc.want {
			t.Errorf("%s (method): got %v, want %v", tc.name, got, tc.want)
		}
	}
}
