package grpchandler

import (
	"context"
	"os"
	"testing"

	knowledgev1 "knowledge_ingestion/src/controller/grpchandler/internal/gen/knowledge/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TestSearch_E2E gọi server gRPC thật (API đang chạy) — skip mặc định, chỉ
// chạy khi set GRPC_E2E_ADDR (vd 127.0.0.1:9002) + GRPC_E2E_JWT (token lấy từ
// POST /api/v1/auth/login). Dùng để kiểm chứng end-to-end sau khi đổi
// interceptor/handler, không chạy trong CI thường.
func TestSearch_E2E(t *testing.T) {
	addr := os.Getenv("GRPC_E2E_ADDR")
	token := os.Getenv("GRPC_E2E_JWT")
	if addr == "" || token == "" {
		t.Skip("bỏ qua e2e: thiếu GRPC_E2E_ADDR hoặc GRPC_E2E_JWT")
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	stub := knowledgev1.NewKnowledgeServiceClient(conn)

	// Không token → Unauthenticated (interceptor chặn).
	_, err = stub.Search(context.Background(), &knowledgev1.SearchRequest{Query: "x"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("thiếu token phải Unauthenticated, got %v", err)
	}

	// Có token → kết quả thật.
	mdCtx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		MetadataAuthorization, "Bearer "+token,
		MetadataTraceID, "e2e-check-001",
	))

	// RebuildVectors không cần Ollama nên e2e được ngay — dùng id không tồn
	// tại để chứng minh đường đi mà không đụng data thật (reset 0 bài).
	rb, err := stub.RebuildVectors(mdCtx, &knowledgev1.RebuildVectorsRequest{ArticleIds: []int64{999999}})
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if rb.GetResetArticles() != 0 {
		t.Fatalf("id ảo phải reset 0 bài, got %d", rb.GetResetArticles())
	}

	// Search cần Ollama sống (embed câu hỏi) — tunnel chết thì fail ở đây,
	// không liên quan đường gRPC (đã chứng minh ở 2 bước trên).
	res, err := stub.Search(mdCtx, &knowledgev1.SearchRequest{Query: "Go Fx", Limit: 3})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	t.Logf("total=%d", res.GetTotal())
	for _, h := range res.GetHits() {
		t.Logf("[%.3f] article=%d chunk=%d text=%.60s", h.GetScore(), h.GetArticleId(), h.GetChunkIndex(), h.GetText())
	}
}
