package grpchandler

import (
	"context"
	"strings"

	"knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/controller/services"

	knowledgev1 "knowledge_ingestion/src/controller/grpchandler/internal/gen/knowledge/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implement knowledge.v1.KnowledgeService — mỏng như controller gin:
// map proto <-> DTO rồi gọi cùng I-service, map Result <-> grpc status.
// Compile-time check: thiếu method là gãy build khi proto thêm rpc mới.
var _ knowledgev1.KnowledgeServiceServer = (*Server)(nil)

type Server struct {
	knowledgev1.UnimplementedKnowledgeServiceServer
	searchSvc services.ISearchService
	adminSvc  services.IArticleAdminService
}

func NewServer(searchSvc services.ISearchService, adminSvc services.IArticleAdminService) *Server {
	return &Server{searchSvc: searchSvc, adminSvc: adminSvc}
}

// NewGRPCServer dựng grpc.Server đã gắn interceptor auth + register service
// — cmd chỉ gọi hàm này nên gen code được giấu trong internal, không ai
// import trực tiếp từ ngoài cây grpchandler.
func NewGRPCServer(srv *Server, auth middlewares.IAuthMiddleware) *grpc.Server {
	s := grpc.NewServer(grpc.UnaryInterceptor(UnaryAuthInterceptor(auth)))
	knowledgev1.RegisterKnowledgeServiceServer(s, srv)
	return s
}

// grpcStatus map lỗi service sang grpc status qua mã trung lập Code (0/400/
// 404/...) — cùng bảng mã mà HTTP Render dùng cho envelope, 2 transport báo
// lỗi nhất quán. Lỗi lạ (nil Code) thì Internal + log kèm trace như Render.
func grpcStatus(ctx context.Context, err error) error {
	if e := errors.From(err); e != nil {
		var code codes.Code
		switch e.GetCode() {
		case errors.BadRequest:
			code = codes.InvalidArgument
		case 404:
			code = codes.NotFound
		default:
			code = codes.Internal
		}
		return status.Error(code, e.Message)
	}
	logs.Error(err, "unhandled error", "trace_id", utils.TraceIDFromCtx(ctx))
	return status.Error(codes.Internal, "internal server error")
}

// Search tương đương POST /api/v1/search.
func (s *Server) Search(ctx context.Context, req *knowledgev1.SearchRequest) (*knowledgev1.SearchResponse, error) {
	res := s.searchSvc.Search(ctx, &dtos.SearchRequest{
		Query:          req.GetQuery(),
		CategoryID:     req.GetCategoryId(),
		Limit:          int(req.GetLimit()),
		ScoreThreshold: req.GetScoreThreshold(),
	})
	if res.Err != nil {
		return nil, grpcStatus(ctx, res.Err)
	}
	out := &knowledgev1.SearchResponse{
		Hits:  make([]*knowledgev1.SearchHit, 0, len(res.Data.Hits)),
		Total: int32(res.Data.Total),
	}
	for _, h := range res.Data.Hits {
		out.Hits = append(out.Hits, &knowledgev1.SearchHit{
			ArticleId:  h.ArticleID,
			ChunkIndex: int32(h.ChunkIndex),
			PageNum:    int32(h.PageNum),
			Score:      h.Score,
			Text:       h.Text,
		})
	}
	return out, nil
}

// RebuildVectors tương đương POST /api/v1/admin/rebuild-vectors.
func (s *Server) RebuildVectors(ctx context.Context, req *knowledgev1.RebuildVectorsRequest) (*knowledgev1.RebuildVectorsResponse, error) {
	res := s.adminSvc.RebuildVectors(ctx, &dtos.RebuildVectorsRequest{
		ArticleIDs: req.GetArticleIds(),
		CategoryID: req.GetCategoryId(),
	})
	if res.Err != nil {
		return nil, grpcStatus(ctx, res.Err)
	}
	return &knowledgev1.RebuildVectorsResponse{
		ResetArticles: res.Data.ResetArticles,
		ArticleIds:    res.Data.ArticleIDs,
	}, nil
}

// bearer cắt "Bearer " khỏi metadata authorization — Python client gửi
// authorization: Bearer <JWT> giống hệt header HTTP.
func bearer(mdValue string) (string, bool) {
	token, ok := strings.CutPrefix(mdValue, "Bearer ")
	if !ok || strings.TrimSpace(token) == "" {
		return "", false
	}
	return strings.TrimSpace(token), true
}
