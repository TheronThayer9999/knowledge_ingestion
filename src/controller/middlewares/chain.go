package middlewares

import (
	"github.com/gin-gonic/gin"
)

// File này là "bảng điều khiển" middleware của service — muốn thêm middleware
// mới (rate limit, request-id, audit...) chỉ cần 3 bước:
//  1. Viết interface + struct + constructor trong file riêng, dạng
//     NewXxx(...) IXxxMiddleware (xem auth.go/cors.go làm mẫu).
//  2. Đăng ký constructor vào loadMiddleware trong src/loader/loader.go.
//  3. Thêm 1 dòng .Handler() vào Global (mọi request) hoặc Protected
//     (chỉ route cần đăng nhập) dưới đây, đúng vị trí thực thi.

// Global trả về middleware chạy cho MỌI request, kể cả preflight OPTIONS
// (preflight không mang token nên Auth không được ở đây). Thứ tự trong
// slice = thứ tự thực thi — trace đứng đầu để mọi log sau đều có trace_id.
func Global(cors ICORSMiddleware, trace ITraceMiddleware) []gin.HandlerFunc {
	return []gin.HandlerFunc{
		gin.Recovery(),
		trace.Handler(),
		cors.Handler(),
	}
}

// Protected gom middleware cho route cần đăng nhập. Sau này thêm rate limit
// thì chỉ cần 1 dòng ở đây, ví dụ:
//
//	return []gin.HandlerFunc{
//		auth.Handler(),
//		ratelimit.Handler(),
//	}
func Protected(auth IAuthMiddleware) []gin.HandlerFunc {
	return []gin.HandlerFunc{
		auth.Handler(),
	}
}
