package grpchandler

import (
	"context"
	"strings"

	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/controller/middlewares"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Metadata keys nói chuyện với Python client — tên giữ giống hệt HTTP
// (authorization) + x-trace-id 2 chiều để nối trace xuyên transport.
const (
	// MetadataAuthorization mang "Bearer <JWT>" (token lấy từ POST /auth/login).
	MetadataAuthorization = "authorization"
	// MetadataTraceID nối trace từ agent — trống thì server tự sinh UUIDv7 và
	// trả về trong header để agent lần log.
	MetadataTraceID = "x-trace-id"
)

// UnaryAuthInterceptor verify JWT + gắn identity/trace vào ctx trước khi vào
// handler — tương đương middlewares.Protected bên HTTP: cùng VerifyToken
// (chữ ký + hạn + đối chiếu DB) nên thu hồi token tức thì như nhau.
func UnaryAuthInterceptor(auth middlewares.IAuthMiddleware) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		token, ok := bearer(firstMeta(md, MetadataAuthorization))
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "thiếu token xác thực")
		}
		userID, err := auth.VerifyToken(ctx, token)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "token không hợp lệ hoặc đã hết hạn")
		}
		ctx = middlewares.WithUserID(ctx, userID)
		traceID := strings.TrimSpace(firstMeta(md, MetadataTraceID))
		if traceID == "" {
			if traceID, err = utils.NewUUIDv7(); err != nil {
				return nil, status.Error(codes.Internal, "internal server error")
			}
		}
		ctx = utils.ContextWithTraceID(ctx, traceID)
		_ = grpc.SetHeader(ctx, metadata.Pairs(MetadataTraceID, traceID))
		logs.Infow("grpc: gọi", "trace_id", traceID, "user_id", userID)
		return handler(ctx, req)
	}
}

// firstMeta lấy giá trị metadata đầu tiên — key vắng thì chuỗi rỗng.
func firstMeta(md metadata.MD, key string) string {
	if vals := md.Get(key); len(vals) > 0 {
		return vals[0]
	}
	return ""
}
