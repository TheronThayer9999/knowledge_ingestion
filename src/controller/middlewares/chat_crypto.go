package middlewares

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	apperrors "knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/dtos"

	"github.com/gin-gonic/gin"
)

// SessionHeader là header mang id phiên mã hoá. Phải có trên MỌI request trong
// phạm vi (kể cả GET không body) vì nó là thứ duy nhất báo server hai việc:
// body này mở bằng khoá nào, và response có cần niêm phong lại hay không.
const SessionHeader = "X-KI-Session"

// SessionStaleValue là giá trị gắn vào SessionHeader của response khi sid hỏng
// (lạ / hết hạn / của người khác) — FE đọc header này thay vì đoán từ message,
// rồi handshake một lần và retry đúng một lượt. Cùng tên với header ở chiều đi là
// chủ ý: một header duy nhất cho "trạng thái phiên".
const SessionStaleValue = "stale"

// envelopeProbe là trần kích thước phong bì: phong bì to bất thường là client
// lỗi hoặc ai đó nhét rác, không có lý do gì đọc hết vào RAM.
const envelopeProbeLimit = 8 << 20 // 8 MB

// ICryptoSessions là vai hẹp mà tầng middleware cần, khai báo ngay bên tiêu
// dùng (DIP) — middlewares KHÔNG được import services vì services đang import
// middlewares, vòng quay tròn sẽ làm build fail.
type ICryptoSessions interface {
	Mode() string
	SessionKey(ctx context.Context, sid string) (key []byte, ok bool)
}

// IChatCryptoMiddleware hợp đồng cho router: một Handler duy nhất.
type IChatCryptoMiddleware interface {
	Handler() gin.HandlerFunc
}

type chatCryptoMiddleware struct {
	sessions ICryptoSessions
}

func NewChatCryptoMiddleware(sessions ICryptoSessions) IChatCryptoMiddleware {
	return &chatCryptoMiddleware{sessions: sessions}
}

// Handler mở phong bì request và niêm phong response cho nhóm API "khách hỏi AI". Đặt SAU auth trong chain
// Protected vì nó cần user_id đã verify để từ chối sid của người khác.
//
// Ba đường đi của một request trong phạm vi:
//   - không sid, body plaintext  → mode required thì 400, còn lại chạy như cũ;
//   - có sid hợp lệ + body JSON   → mở phong bì, thay body bằng plaintext;
//   - có sid hợp lệ               → response được niêm phong lại (kể cả khi body
//     rỗng như GET, hoặc multipart như /chat/attachments — tệp vẫn đi plaintext
//     vì dựng phong bì multipart là đổi cả pipeline trích tệp, chỉ phần JSON trả
//     về là bị khoá).
func (m *chatCryptoMiddleware) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		if m.sessions.Mode() == config.ChatCryptoOff || !inScope(c.Request.URL.Path) {
			c.Next()
			return
		}
		sid := strings.TrimSpace(c.GetHeader(SessionHeader))
		key, ok := m.sessions.SessionKey(c.Request.Context(), sid)
		if sid != "" && !ok {
			abortStale(c)
			return
		}
		if sid == "" {
			if m.sessions.Mode() == config.ChatCryptoRequired {
				abortStale(c)
				return
			}
			c.Next()
			return
		}
		plain, err := m.openRequestBody(c, sid, key)
		if err != nil {
			abortBadRequest(c, err)
			return
		}
		if plain != nil {
			rewriteBody(c, plain)
		}
		m.sealResponse(c, sid, key)
	}
}

// inScope khoá phạm vi mã hoá đúng nhóm "khách hỏi AI": /chat (kể cả
// /chat/attachments) và /conversations*. /crypto/handshake ngoài phạm vi — nó
// là thứ dựng nên khoá, không thể được mã hoá bằng chính khoá đó.
func inScope(path string) bool {
	rel := strings.TrimPrefix(path, "/api/v1")
	return rel == "/chat" || strings.HasPrefix(rel, "/chat/") ||
		strings.HasPrefix(rel, "/conversations")
}

// openRequestBody mở phong bì của body. Trả nil khi không có gì để mở (body
// rỗng hoặc không phải JSON — đường plaintext/multipart), trả plaintext khi mở
// thành công.
func (m *chatCryptoMiddleware) openRequestBody(c *gin.Context, sid string, key []byte) ([]byte, error) {
	if c.Request.Body == nil || !isJSONContent(c.Request) {
		return nil, nil
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, envelopeProbeLimit))
	if err != nil {
		return nil, apperrors.NewCustomHttpError(http.StatusBadRequest, apperrors.BadRequest,
			"không đọc được body")
	}
	env, isEnvelope := probeEnvelope(raw)
	if !isEnvelope {
		// Body JSON thường: chỉ chấp nhận khi mode optional; required đã chặn
		// ở trên nên đường này luôn là plaintext được phép.
		return raw, nil
	}
	if env.SID != sid {
		return nil, apperrors.NewCustomHttpError(http.StatusBadRequest, apperrors.BadRequest,
			"sid trong phong bì không khớp header")
	}
	plain, err := utils.Open(key, utils.AAD(sid, utils.DirRequest), env.Nonce, env.CT)
	if err != nil {
		return nil, err
	}
	return plain, nil
}

// isJSONContent chỉ body JSON mới là ứng viên phong bì — multipart của
// /chat/attachments đi đường plaintext có chủ đích (dựng phong bì cho tệp là
// đổi cả pipeline trích tệp, không phải việc của đợt này).
func isJSONContent(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "application/json")
}

// probeEnvelope nhận diện phong bì bằng đúng 4 field khuôn, không nhận nhầm
// body plaintext có field trùng tên.
func probeEnvelope(raw []byte) (dtos.CryptoEnvelope, bool) {
	var probe struct {
		V     *int    `json:"v"`
		SID   *string `json:"sid"`
		Nonce *string `json:"nonce"`
		CT    *string `json:"ct"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &probe) != nil {
		return dtos.CryptoEnvelope{}, false
	}
	if probe.V == nil || probe.SID == nil || probe.Nonce == nil || probe.CT == nil {
		return dtos.CryptoEnvelope{}, false
	}
	env := dtos.CryptoEnvelope{V: *probe.V, SID: *probe.SID, Nonce: *probe.Nonce, CT: *probe.CT}
	if env.V != 1 || env.SID == "" || env.Nonce == "" || env.CT == "" {
		return dtos.CryptoEnvelope{}, false
	}
	return env, true
}

// rewriteBody thay body đã mở vào request để handler Bind như bình thường —
// toàn bộ tầng dưới (validate, service, agent) chỉ thấy plaintext, không có
// nhánh "phiên bản mã hoá" nào len vào nghiệp vụ.
func rewriteBody(c *gin.Context, plain []byte) {
	c.Request.Body = io.NopCloser(bytes.NewReader(plain))
	c.Request.ContentLength = int64(len(plain))
	c.Request.Header.Set("Content-Type", "application/json")
}

// sealResponse bọc ResponseWriter bằng bản đệm, cho handler ghi thoải mái, rồi
// niêm phong cả body một lần khi chain kết thúc.
func (m *chatCryptoMiddleware) sealResponse(c *gin.Context, sid string, key []byte) {
	buf := newBufferedWriter(c.Writer)
	c.Writer = buf
	c.Next()
	plain := buf.buffered()
	if len(plain) == 0 {
		buf.flushStatusOnly()
		return
	}
	nonce, ct, err := utils.Seal(key, utils.AAD(sid, utils.DirResponse), plain)
	if err != nil {
		// Không được để lộ plaintext khi không niêm phong nổi: log rồi trả 500
		// sạch, body gốc không xuống dây.
		logs.Error(err, "chat_crypto: niêm phong response lỗi", "sid_prefix", sidPrefix(sid))
		buf.abortPlain()
		return
	}
	raw, err := json.Marshal(dtos.CryptoEnvelope{V: 1, SID: sid, Nonce: nonce, CT: ct})
	if err != nil {
		logs.Error(err, "chat_crypto: dựng phong bì response lỗi", "sid_prefix", sidPrefix(sid))
		buf.abortPlain()
		return
	}
	buf.seal(raw)
}

// abortBadRequest trả 400 với message của lỗi đã được đóng gói (mọi message ở
// đây đều là chuỗi tự viết, không rò chi tiết crypto của phong bì).
func abortBadRequest(c *gin.Context, err error) {
	e := apperrors.From(err)
	if e == nil {
		e = apperrors.NewCustomHttpError(http.StatusBadRequest, apperrors.BadRequest,
			"phong bì không hợp lệ")
	}
	logs.Warnw("chat_crypto: request không mở được", "status", e.GetHttpCode(),
		"trace_id", utils.TraceIDFromCtx(c.Request.Context()))
	c.AbortWithStatusJSON(e.GetHttpCode(), dtos.ResponseResource{
		Status: false, Code: e.GetCode(), Message: e.Message,
	})
}

func abortStale(c *gin.Context) {
	// Header stale đặt TRƯỚC khi abort: FE đọc từ response, không phụ thuộc
	// message. 400 chứ không 401 — token vẫn hạn, 401 sẽ làm interceptor chạy
	// luồng refresh rồi retry vô ích.
	c.Header(SessionHeader, SessionStaleValue)
	c.AbortWithStatusJSON(http.StatusBadRequest, dtos.ResponseResource{
		Status:  false,
		Code:    apperrors.BadRequest,
		Message: "cần mở lại phiên mã hoá",
	})
}

func sidPrefix(sid string) string {
	if len(sid) > 8 {
		return sid[:8]
	}
	return sid
}

// bufferedWriter là gin.ResponseWriter có đệm: handler ghi vào buffer chứ không
// xuống socket, nên ta lấy được nguyên body để niêm phong một khối.
type bufferedWriter struct {
	gin.ResponseWriter
	body   bytes.Buffer
	status int
}

func newBufferedWriter(w gin.ResponseWriter) *bufferedWriter {
	return &bufferedWriter{ResponseWriter: w, status: http.StatusOK}
}

func (b *bufferedWriter) Write(data []byte) (int, error) { return b.body.Write(data) }

// WriteHeader chỉ ghi nhận, chưa đổ xuống socket — gin gọi nó qua c.Status().
func (b *bufferedWriter) WriteHeader(code int) { b.status = code }

// WriteHeaderNow bị chặn: mọi đường ghi trực tiếp đều phải đi qua seal/flush.
func (b *bufferedWriter) WriteHeaderNow() {}

func (b *bufferedWriter) Written() bool { return b.body.Len() > 0 }

func (b *bufferedWriter) Status() int { return b.status }

func (b *bufferedWriter) Size() int { return b.body.Len() }

func (b *bufferedWriter) WriteString(s string) (int, error) { return b.body.WriteString(s) }

func (b *bufferedWriter) buffered() []byte { return b.body.Bytes() }

// seal ghi bản niêm phong xuống writer thật với đúng status handler đã đặt.
func (b *bufferedWriter) seal(raw []byte) {
	b.ResponseWriter.Header().Set("Content-Type", "application/json; charset=utf-8")
	b.ResponseWriter.WriteHeader(b.status)
	if _, err := b.ResponseWriter.Write(raw); err != nil {
		logs.Warnw("chat_crypto: ghi response lỗi", "error", err)
	}
}

// flushStatusOnly dùng cho response không body (204/304): không có gì để niêm
// phong, chỉ cần đổ status đã ghi nhận.
func (b *bufferedWriter) flushStatusOnly() {
	b.ResponseWriter.WriteHeader(b.status)
}

// abortPlain dành cho tình huống niêm phong thất bại: thân plaintext đang nằm
// trong buffer TUYỆT ĐỐI không được xuống dây, nên bỏ luôn body, chỉ ghi 500.
func (b *bufferedWriter) abortPlain() {
	b.body.Reset()
	b.ResponseWriter.Header().Set("Content-Type", "application/json; charset=utf-8")
	b.ResponseWriter.WriteHeader(http.StatusInternalServerError)
}
