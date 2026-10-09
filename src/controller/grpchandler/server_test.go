package grpchandler

import (
	"context"
	stderrors "errors"
	"net/http"
	"testing"

	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/services"

	knowledgev1 "knowledge_ingestion/src/controller/grpchandler/internal/gen/knowledge/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Fake gọn — chỉ implement đúng method handler gọi.
type fakeGrpcSearchSvc struct {
	res *dtos.SearchResponse
	err error
}

func (f *fakeGrpcSearchSvc) Search(_ context.Context, _ *dtos.SearchRequest) dtos.Result[*dtos.SearchResponse] {
	if f.err != nil {
		return dtos.Fail[*dtos.SearchResponse](f.err)
	}
	return dtos.Ok(f.res)
}

type fakeGrpcAdminSvc struct {
	res *dtos.RebuildVectorsResponse
	err error
}

func (f *fakeGrpcAdminSvc) RebuildVectors(_ context.Context, _ *dtos.RebuildVectorsRequest) dtos.Result[*dtos.RebuildVectorsResponse] {
	if f.err != nil {
		return dtos.Fail[*dtos.RebuildVectorsResponse](f.err)
	}
	return dtos.Ok(f.res)
}

var (
	_ services.ISearchService        = (*fakeGrpcSearchSvc)(nil)
	_ services.IArticleAdminService = (*fakeGrpcAdminSvc)(nil)
)

// Search map DTO sang proto đủ field.
func TestServer_Search(t *testing.T) {
	s := NewServer(
		&fakeGrpcSearchSvc{res: &dtos.SearchResponse{
			Hits:  []*dtos.SearchHitResponse{{ArticleID: 8, ChunkIndex: 11, PageNum: 0, Score: 0.83, Text: "đoạn"}},
			Total: 1,
		}},
		&fakeGrpcAdminSvc{},
	)
	out, err := s.Search(context.Background(), &knowledgev1.SearchRequest{Query: "x", Limit: 5})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.GetTotal() != 1 || len(out.GetHits()) != 1 {
		t.Fatalf("response sai: %v", out)
	}
	h := out.GetHits()[0]
	if h.GetArticleId() != 8 || h.GetChunkIndex() != 11 || h.GetScore() != 0.83 || h.GetText() != "đoạn" {
		t.Fatalf("hit sai: %v", h)
	}
}

// Lỗi service 400 → InvalidArgument, giữ message.
func TestServer_SearchError(t *testing.T) {
	s := NewServer(
		&fakeGrpcSearchSvc{err: errors.NewCustomHttpError(http.StatusBadRequest, errors.BadRequest, "query rỗng")},
		&fakeGrpcAdminSvc{},
	)
	_, err := s.Search(context.Background(), &knowledgev1.SearchRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("400 phải InvalidArgument, got %v", err)
	}
	if got := status.Convert(err).Message(); got != "query rỗng" {
		t.Fatalf("message sai: %q", got)
	}
}

// Lỗi lạ (không phải errors.Error) → Internal.
func TestServer_SearchInternal(t *testing.T) {
	s := NewServer(
		&fakeGrpcSearchSvc{err: stderrors.New("ollama down")},
		&fakeGrpcAdminSvc{},
	)
	_, err := s.Search(context.Background(), &knowledgev1.SearchRequest{Query: "x"})
	if status.Code(err) != codes.Internal {
		t.Fatalf("lỗi lạ phải Internal, got %v", err)
	}
}

// Rebuild map đủ field.
func TestServer_RebuildVectors(t *testing.T) {
	s := NewServer(
		&fakeGrpcSearchSvc{},
		&fakeGrpcAdminSvc{res: &dtos.RebuildVectorsResponse{ResetArticles: 2, ArticleIDs: []int64{8, 9}}},
	)
	out, err := s.RebuildVectors(context.Background(), &knowledgev1.RebuildVectorsRequest{ArticleIds: []int64{8, 9}})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out.GetResetArticles() != 2 || len(out.GetArticleIds()) != 2 {
		t.Fatalf("response sai: %v", out)
	}
}
