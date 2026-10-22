package middlewares

import (
	"net/http"
	"sync"
	"time"

	"knowledge_ingestion/src/controller/dtos"

	"github.com/gin-gonic/gin"
)

// IRateLimiter giới hạn số request trong một cửa sổ thời gian — dùng cho
// endpoint nhạy cảm (login) để chống brute-force. State lưu trong memory
// (mất khi restart) — đủ cho 1 instance; đa instance cần Redis.
type IRateLimiter interface {
	Handler() gin.HandlerFunc
}

type rateLimiter struct {
	mu      sync.Mutex
	clients map[string]*bucket
	limit   int
	window  time.Duration
}

type bucket struct {
	count int
	reset time.Time
}

// NewRateLimiter tạo limiter cho login: 5 lần / phút / IP. fx Provide gọi
// constructor không tham số.
func NewRateLimiter() IRateLimiter {
	limit := 5
	window := time.Minute
	return &rateLimiter{
		clients: make(map[string]*bucket),
		limit:   limit,
		window:  window,
	}
}

func (r *rateLimiter) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		r.mu.Lock()
		b, ok := r.clients[ip]
		now := time.Now()
		if !ok || now.After(b.reset) {
			b = &bucket{count: 0, reset: now.Add(r.window)}
			r.clients[ip] = b
		}
		b.count++
		over := b.count > r.limit
		r.mu.Unlock()
		if over {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, dtos.ResponseResource{
				Status:  false,
				Code:    http.StatusTooManyRequests,
				Message: "quá nhiều yêu cầu, vui lòng thử lại sau",
			})
			return
		}
		c.Next()
	}
}