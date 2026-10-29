package services

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	apperrors "knowledge_ingestion/src/common/errors"
	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/common/utils"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/controller/middlewares"
	"knowledge_ingestion/src/infrastructure/caches"

	"github.com/redis/go-redis/v9"
)

// IChatCryptoService dựng và tra khoá phiên mã hoá payload cho nhóm API "khách
// hỏi AI" (Đợt 29).
//
// Mô hình đã chốt: client sinh cặp X25519, gửi public key lên Handshake, server
// trả public key ephemeral của nó + session id; hai phía HKDF ra cùng một khoá
// AES-256-GCM. Server GIỮ khoá (trong store) nên giải mã được body — RAG, phân
// loại câu hỏi, trích dẫn vẫn chạy như cũ; thứ bị khoá lại là kẻ nghe lén trên
// đường truyền và log của các trung gian.
type IChatCryptoService interface {
	// Mode trả "off"/"optional"/"required" — middleware hỏi mỗi request.
	Mode() string
	// Handshake đổi public key client lấy một phiên mã hoá (cần đăng nhập).
	Handshake(ctx context.Context, req *dtos.CryptoHandshakeRequest) dtos.Result[*dtos.CryptoHandshakeResponse]
	// SessionKey tra khoá của sid. ok=false khi sid lạ, hết hạn, hoặc thuộc
	// user khác — caller trả 400 kèm header báo stale (KHÔNG 401: 401 làm FE
	// chạy vào luồng refresh token, ở đây token vẫn hoàn toàn hợp lệ).
	SessionKey(ctx context.Context, sid string) (key []byte, ok bool)
}

// sessionIDLen là độ dài sid do utils.NewSessionID sinh ra (hex 16 byte).
const sessionIDLen = 32

// sessionRecord là phần lưu ở store. Khoá để base64 vì giá trị đi vào Redis;
// UserID để đối chiếu chủ sở hữu ở mỗi request.
type sessionRecord struct {
	UserID int64  `json:"uid"`
	KeyB64 string `json:"key"`
}

// sessionStore là khe lưu khoá phiên. Production dùng Redis (mọi instance cùng
// thấy một phiên, restart API không đá người dùng đang chat); bản in-memory là
// đường lùi cho máy dev không có Redis.
type sessionStore interface {
	Put(ctx context.Context, rec sessionRecord, sid string, ttl time.Duration) error
	Get(ctx context.Context, sid string) (sessionRecord, bool)
}

type chatCryptoService struct {
	mode        string
	ttl         time.Duration
	store       sessionStore
	currentUser middlewares.ICurrentUser
}

// NewChatCryptoService đọc chat_crypto từ config và chọn store theo Redis có
// sẵn hay không. Không có Redis thì log WARN MỘT lần lúc boot: khoá phiên chỉ
// sống trong tiến trình này, deploy nhiều instance sẽ lệch phiên.
func NewChatCryptoService(cfg config.IConfig, cache caches.ICache,
	currentUser middlewares.ICurrentUser) IChatCryptoService {
	cc := cfg.GetChatCrypto()
	var client *redis.Client
	if cache != nil {
		client = cache.GetClient()
	}
	if client == nil {
		logs.Warnw("chat_crypto: không có Redis — khoá phiên chỉ sống trong tiến trình này, " +
			"deploy nhiều instance sẽ lệch phiên")
	}
	return newChatCryptoService(cc.Mode, time.Duration(cc.SessionTTLMinutes)*time.Minute,
		newSessionStore(client, cfg.GetJWT().Secret), currentUser)
}

// newChatCryptoService là constructor thật: NewChatCryptoService chỉ lo chọn
// store theo config, còn test dựng service với store in-memory và TTL ngắn mà
// không cần Redis.
func newChatCryptoService(mode string, ttl time.Duration, store sessionStore,
	currentUser middlewares.ICurrentUser) *chatCryptoService {
	return &chatCryptoService{mode: mode, ttl: ttl, store: store, currentUser: currentUser}
}

func (s *chatCryptoService) Mode() string { return s.mode }

// Handshake là endpoint duy nhất KHÔNG nằm trong phong bì (nó dựng chính phong
// bì), nên body là plaintext. Public key sai là lỗi input → 400 với message đã
// gọt, không lộ chi tiết crypto.
func (s *chatCryptoService) Handshake(ctx context.Context,
	req *dtos.CryptoHandshakeRequest) dtos.Result[*dtos.CryptoHandshakeResponse] {
	if s.mode == config.ChatCryptoOff {
		// FE đọc mode này để khỏi dựng khoá. Trả 200 + session_id rỗng còn đúng
		// hơn 400: "không mã hoá" là trạng thái hợp lệ của hệ thống.
		return dtos.Ok(&dtos.CryptoHandshakeResponse{Mode: s.mode})
	}
	userID, ok := s.currentUser.UserID(ctx)
	if !ok {
		// 400 chứ không 401: route đã bị auth chặn trước rồi, vào tới đây mà
		// ctx không có user_id nghĩa là chain bị gắn sai (lỗi lập trình), còn
		// 401 sẽ làm FE hiểu nhầm là token hết hạn và chạy luồng refresh.
		return dtos.Fail[*dtos.CryptoHandshakeResponse](
			apperrors.NewCustomHttpError(http.StatusBadRequest, apperrors.BadRequest,
				"chưa xác thực để mở phiên mã hoá"))
	}
	peer, err := utils.NewE2EEKeyPair()
	if err != nil {
		return dtos.Fail[*dtos.CryptoHandshakeResponse](err)
	}
	sid, err := utils.NewSessionID()
	if err != nil {
		return dtos.Fail[*dtos.CryptoHandshakeResponse](err)
	}
	key, err := peer.SharedKey(strings.TrimSpace(req.ClientPublic), sid)
	if err != nil {
		if errors.Is(err, utils.ErrPeerKey) {
			return dtos.Fail[*dtos.CryptoHandshakeResponse](
				apperrors.NewCustomHttpError(http.StatusBadRequest, apperrors.BadRequest,
					"client_public không phải khoá X25519 hợp lệ"))
		}
		return dtos.Fail[*dtos.CryptoHandshakeResponse](err)
	}
	rec := sessionRecord{UserID: userID, KeyB64: base64.StdEncoding.EncodeToString(key)}
	if err := s.store.Put(ctx, rec, sid, s.ttl); err != nil {
		return dtos.Fail[*dtos.CryptoHandshakeResponse](err)
	}
	logs.Infow("chat_crypto: mở phiên", "user_id", userID, "sid", sid,
		"expires_in_sec", int(s.ttl.Seconds()))
	return dtos.Ok(&dtos.CryptoHandshakeResponse{
		SessionID:    sid,
		ServerPublic: peer.PublicKeyB64(),
		ExpiresAt:    time.Now().Add(s.ttl).UTC().Format(time.RFC3339),
		Mode:         s.mode,
	})
}

// SessionKey tra khoá cho middleware, kèm ràng buộc chủ sở hữu: sid lấy được
// tab/log request của người khác không mở được khoá của họ — coi như stale.
func (s *chatCryptoService) SessionKey(ctx context.Context, sid string) ([]byte, bool) {
	sid = strings.TrimSpace(sid)
	// Sid là hex 32 ký tự server tự sinh: chuỗi khác khuôn đến từ client lỗi
	// hoặc kẻ mò, không cần xuống tận store.
	if len(sid) != sessionIDLen {
		return nil, false
	}
	// Sid là hex server tự sinh: chuỗi khác khuôn đến từ client lỗi hoặc kẻ mò
	// id, không cần xuống tận store.
	if _, err := hex.DecodeString(sid); err != nil {
		return nil, false
	}
	rec, ok := s.store.Get(ctx, sid)
	if !ok {
		return nil, false
	}
	if uid, have := s.currentUser.UserID(ctx); !have || uid != rec.UserID {
		// Không log "sid của ai" — chỉ đủ để điều tra, không đủ để lộ danh tính.
		logs.Warnw("chat_crypto: sid không thuộc user hiện tại", "sid_prefix", safePrefix(sid))
		return nil, false
	}
	key, err := base64.StdEncoding.DecodeString(rec.KeyB64)
	if err != nil || len(key) != 32 {
		logs.Warnw("chat_crypto: khoá trong store hỏng", "sid_prefix", safePrefix(sid))
		return nil, false
	}
	return key, true
}

// newSessionStore chọn backend theo Redis có hay không; secret rỗng thì không
// seal được → vẫn dùng Redis nhưng cảnh báo (config fail-fast ở Load nên đường
// này chỉ xảy ra khi ai đó tự chế cache client nil).
func newSessionStore(client *redis.Client, secret string) sessionStore {
	if client == nil {
		return newMemorySessionStore()
	}
	if secret == "" {
		logs.Warnw("chat_crypto: JWT secret trống — khoá phiên lưu Redis không được mã hoá at-rest")
	}
	return &redisSessionStore{client: client, secret: secret}
}

// -----------------------------------------------------------------------------
// Store Redis
// -----------------------------------------------------------------------------

// e2eeKeyPrefix đặt trước sid để khoá phiên không lẫn với key cache nghiệp vụ
// nào khác trong cùng DB Redis.
const e2eeKeyPrefix = "e2ee:sid:"

type redisSessionStore struct {
	client *redis.Client
	secret string
}

// Put lưu record đã mã hoá at-rest bằng đúng helper đã dùng cho API key
// (SealString + JWT secret). Dump Redis hay log MONITOR vì thế không cho ra
// khoá dùng được.
func (s *redisSessionStore) Put(ctx context.Context, rec sessionRecord, sid string, ttl time.Duration) error {
	payload, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("nén khoá phiên: %w", err)
	}
	value := string(payload)
	if s.secret != "" {
		value, err = utils.SealString(string(payload), s.secret)
		if err != nil {
			return fmt.Errorf("mã hoá khoá phiên: %w", err)
		}
	}
	if err := s.client.Set(ctx, e2eeKeyPrefix+sid, value, ttl).Err(); err != nil {
		return fmt.Errorf("lưu khoá phiên: %w", err)
	}
	return nil
}

func (s *redisSessionStore) Get(ctx context.Context, sid string) (sessionRecord, bool) {
	var rec sessionRecord
	value, err := s.client.Get(ctx, e2eeKeyPrefix+sid).Result()
	if err != nil {
		if !errors.Is(err, redis.Nil) { // redis.Nil = phiên hết hạn: đường bình thường
			logs.Warnw("chat_crypto: đọc store lỗi", "sid_prefix", safePrefix(sid), "error", err)
		}
		return rec, false
	}
	if s.secret != "" {
		value, err = utils.OpenString(value, s.secret)
		if err != nil {
			// Đổi JWT_SECRET làm mọi phiên cũ mở không được — client sẽ nhận
			// stale và handshake lại, không mất dữ liệu.
			logs.Warnw("chat_crypto: giải mã khoá lưu lỗi", "sid_prefix", safePrefix(sid), "error", err)
			return rec, false
		}
	}
	if err := json.Unmarshal([]byte(value), &rec); err != nil {
		logs.Warnw("chat_crypto: record phiên hỏng", "sid_prefix", safePrefix(sid), "error", err)
		return rec, false
	}
	return rec, true
}

// -----------------------------------------------------------------------------
// Store in-memory (đường lùi)
// -----------------------------------------------------------------------------

type memorySessionStore struct {
	mu     sync.Mutex
	bySID  map[string]sessionRecord
	expire map[string]time.Time
}

func newMemorySessionStore() *memorySessionStore {
	return &memorySessionStore{bySID: map[string]sessionRecord{}, expire: map[string]time.Time{}}
}

func (m *memorySessionStore) Put(_ context.Context, rec sessionRecord, sid string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bySID[sid] = rec
	m.expire[sid] = time.Now().Add(ttl)
	return nil
}

func (m *memorySessionStore) Get(_ context.Context, sid string) (sessionRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.bySID[sid]
	if !ok {
		return sessionRecord{}, false
	}
	if time.Now().After(m.expire[sid]) {
		delete(m.bySID, sid)
		delete(m.expire, sid)
		return sessionRecord{}, false
	}
	return rec, true
}

// safePrefix cắt sid làm nhãn log mà không nguyên văn (sid là định danh phiên).
func safePrefix(sid string) string {
	if len(sid) > 8 {
		return sid[:8]
	}
	return sid
}
