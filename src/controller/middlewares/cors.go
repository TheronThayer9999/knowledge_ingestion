package middlewares

import (
	"net/http"

	"knowledge_ingestion/src/config"

	"github.com/gin-gonic/gin"
)

// ICORSMiddleware là contract middleware CORS — constructor NewCORSMiddleware
// đọc config một lần, Handler trả gin.HandlerFunc gắn vào chain.
type ICORSMiddleware interface {
	Handler() gin.HandlerFunc
}

type corsMiddleware struct {
	allowed map[string]struct{}
}

// NewCORSMiddleware dựng CORS từ config — fx gọi một lần lúc startup, mọi
// request sau dùng chung map origin (chỉ đọc nên an toàn concurrent).
func NewCORSMiddleware(cfg config.IConfig) ICORSMiddleware {
	origins := cfg.GetCORS().AllowedOrigins
	allowed := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		allowed[o] = struct{}{}
	}
	return &corsMiddleware{allowed: allowed}
}

// Handler trả middleware mở cross-origin cho đúng danh sách config —
// origin lạ không được gắn header nên trình duyệt tự chặn. Preflight
// OPTIONS được trả 204 ngay tại đây, không đi tiếp vào chain.
func (m *corsMiddleware) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if _, ok := m.allowed[origin]; ok && origin != "" {
			h := c.Writer.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Vary", "Origin")
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization")
			h.Set("Access-Control-Max-Age", "86400")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
