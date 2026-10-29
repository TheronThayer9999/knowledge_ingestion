package dtos

// Khuôn mã hoá payload cho nhóm API "khách hỏi AI" (Đợt 29).
//
// Hai lớp trên đường dây:
//   /crypto/handshake  — plaintext, dựng khoá phiên X25519 (chính nó KHÔNG thể
//     nằm trong phong bì, nếu không sẽ có vòng lặp bootstrap).
//   mọi request trong phạm vi — body/response là CryptoEnvelope, khoá do hai
//     phía vừa thoả thuận.

// CryptoHandshakeRequest mang public key X25519 của client (32 byte, base64).
// Server không lưu private key của client — chỉ đủ để tính shared secret.
type CryptoHandshakeRequest struct {
	// 44 ký tự là base64 có padding của đúng 32 byte; chặn sớm để không phải
	// gọi crypto ở service với chuỗi rác.
	ClientPublic string `json:"client_public" binding:"required,min=43,max=44" example:"a1V3..."`
}

// CryptoHandshakeResponse: server_public để client tự tính lại cùng một khoá,
// session_id để gửi kèm mọi request sau đó (header X-KI-Session).
//
// Mode trả về để FE quyết định có cần mã hoá nữa không: "off" thì session_id
// rỗng và interceptor đi đường plaintext — không bắt FE phải gọi thêm endpoint
// cấu hình (endpoint đó sẽ lại cần bảo mật).
type CryptoHandshakeResponse struct {
	SessionID    string `json:"session_id,omitempty" example:"3f2a…"`
	ServerPublic string `json:"server_public,omitempty" example:"b2V4…"`
	// ExpiresAt là RFC3339 của hạn phiên — chuỗi chứ không phải time.Time để
	// omitempty có tác dụng (mode "off" thì cả ba field rỗng, FE đi plaintext).
	ExpiresAt string `json:"expires_at,omitempty" example:"2026-10-28T18:00:00Z"`
	Mode      string `json:"mode" example:"optional"`
}

// CryptoEnvelope là phong bì chung cho cả hai chiều.
//
// `v` là phiên bản khuôn (2 khi đổi thuật toán) — client cũ không hiểu bản mới
// thì server báo 400 thay vì giải mã lung tung. Nonce 12 byte ngẫu nhiên cho
// MỖI message, không dùng lại.
type CryptoEnvelope struct {
	V     int    `json:"v" binding:"required,eq=1" example:"1"`
	SID   string `json:"sid" binding:"required,len=32" example:"3f2a…"`
	Nonce string `json:"nonce" binding:"required" example:"cQ1c…"`
	CT    string `json:"ct" binding:"required" example:"7hNq…"`
}
