package middlewares

import (
	"strings"

	"knowledge_ingestion/src/common/utils"

	"github.com/gin-gonic/gin"
)

// Context key + header mang trace_id của request — client/gateway gửi lên
// thì giữ nguyên (truy vết xuyên suốt), không thì server tự sinh.
const CtxTraceID = "trace_id"

// TraceIDHeader header 2 chiều: request mang sang để nối trace từ gateway,
// response trả về để client đối chiếu log khi báo lỗi.
const TraceIDHeader = "X-Trace-ID"

// maxTraceIDLen cắt trace_id client gửi lên — đủ cho UUID, chặn chuỗi rác
// dài làm phình log.
const maxTraceIDLen = 128

// ITraceMiddleware gắn trace_id vào mỗi request — service tầng dưới đọc ra
// qua utils.TraceIDFromCtx mà không cần biết gin (giống ICurrentUser).
type ITraceMiddleware interface {
	// Handler trả middleware chạy đầu chain Global cho MỌI request.
	Handler() gin.HandlerFunc
}

type traceMiddleware struct{}

func NewTraceMiddleware() ITraceMiddleware {
	return &traceMiddleware{}
}

// Handler lấy trace_id client gửi lên (nối trace từ gateway), không có thì
// sinh UUIDv7. Gắn vào gin ctx + request context chuẩn + header response.
// Sinh ID lỗi (thực tế gần như không bao giờ) thì cho request đi tiếp không
// trace — quan sát không được chặn nghiệp vụ.
func (m *traceMiddleware) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID := strings.TrimSpace(c.GetHeader(TraceIDHeader))
		if len(traceID) > maxTraceIDLen {
			traceID = traceID[:maxTraceIDLen]
		}
		if traceID == "" {
			var err error
			traceID, err = utils.NewUUIDv7()
			if err != nil {
				c.Next()
				return
			}
		}
		c.Set(CtxTraceID, traceID)
		c.Header(TraceIDHeader, traceID)
		c.Request = c.Request.WithContext(utils.ContextWithTraceID(c.Request.Context(), traceID))
		c.Next()
	}
}

// TraceID đọc trace_id mà Handler đã gắn — false khi route quên gắn
// middleware hoặc sinh ID lỗi ở trên.
func TraceID(c *gin.Context) (string, bool) {
	id, ok := c.Get(CtxTraceID)
	if !ok {
		return "", false
	}
	s, ok := id.(string)
	return s, ok && s != ""
}
