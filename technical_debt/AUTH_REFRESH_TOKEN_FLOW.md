# Cơ chế luồng refresh token — từng bước

Tài liệu này mô tả **luồng chạy thực tế** của cơ chế refresh token trong repo
(login → gọi API → refresh → reuse → logout → đổi mật khẩu), kèm trạng thái DB sau mỗi
bước, ma trận lỗi và cách FE phối hợp.

- Vì sao cần cơ chế này (giải thích từ đầu): `technical_debt/AUTH_REFRESH_TOKEN.md`
- Code chính: `src/controller/services/user_service.go`,
  `src/infrastructure/repository/refresh_token_repository.go`,
  `src/domain/refresh_token.go`

---

## 0. Tóm tắt 30 giây

```
Login  → cấp access token (24h) + refresh token (30d, lưu hash trong DB)
Gọi API→ chỉ gửi access token
Hết hạn access → gửi refresh token đổi cặp mới (refresh cũ bị đánh dấu đã dùng)
Refresh cũ xuất hiện lần 2 → coi là bị đánh cắp → thu hồi CẢ phiên
Logout → thu hồi refresh token của phiên (access token sống tới hết hạn)
Đổi pass/khoá acc → thu hồi toàn bộ refresh token của user
```

---

## 1. Thành phần tham gia

| Thành phần | Vai trò |
|---|---|
| Client (FE) | Giữ access + refresh token; tự động gọi refresh khi access hết hạn |
| `middlewares.Protected` | Verify access token mỗi request: chữ ký, hạn, `active`, `password_changed_at` |
| `UserAPI` | 4 endpoint: login, refresh, logout, logout-all |
| `userService` | Quyết định sinh/xoay/thu hồi; map mọi lỗi xác thực về 401 chung |
| `RefreshTokenRepository` | Transaction + `FOR UPDATE`; đọc/ghi bảng `refresh_tokens` |
| PostgreSQL | Nguồn sự thật duy nhất về phiên (chỉ lưu hash token) |
| Worker | Job `purge-refresh-tokens` xoá token chết mỗi ngày |

---

## 2. Trạng thái của một dòng refresh token

```
            login
              │
              ▼
        ┌──────────┐   refresh (dùng 1 lần)   ┌──────────┐
        │  ACTIVE  │ ─────────────────────────►│ ROTATED  │
        │rotated=∅ │                            │rotated=t │
        └──────────┘                            └──────────┘
              │                                      │
              │ logout / reuse / đổi pass            │ reuse lần nữa
              ▼                                      ▼
        ┌──────────┐                            (đã thu hồi family)
        │ REVOKED  │◄───────────────────────────┘
        │revoked=t │
        └──────────┘
              │
              │ quá expires_at hoặc family_expires_at
              ▼
        ┌──────────┐
        │ EXPIRED  │  (không dùng được; janitor xoá sau 7 ngày)
        └──────────┘
```

Thứ tự kiểm tra trong `Rotate` (quan trọng khi debug):

1. Không tìm thấy hash → `ErrRefreshNotFound`
2. `revoked_at != NULL` → `ErrRefreshRevoked`
3. `now >= expires_at` **hoặc** `now >= family_expires_at` → `ErrRefreshExpired`
4. `rotated_at != NULL` → `ErrRefreshReuse`

---

## 3. Luồng đăng nhập

```
FE                          API                         PostgreSQL
 │  POST /auth/login         │                              │
 │  {username, password}     │                              │
 ├──────────────────────────►│                              │
 │                           │ SELECT user by username ────►│
 │                           │ bcrypt.Compare               │
 │                           │ sinh family_id (UUIDv7)      │
 │                           │ sinh refresh token (32B)     │
 │                           │ INSERT refresh_tokens ──────►│
 │                           │ (hash, family, 30d/90d)      │
 │                           │ ký access JWT                │
 │◄──────────────────────────┤ { token, refresh_token, ... }│
```

Chi tiết:

- Sai username/mật khẩu → **401 chung** `"sai tài khoản hoặc mật khẩu"` (không lộ username tồn tại).
- `active = false` → **403** `"tài khoản đã bị khóa"`.
- Mỗi lần login tạo **family mới** — đăng nhập ở điện thoại không ảnh hưởng laptop.
- `last_login` cập nhật best-effort: lỗi bước này không làm login thất bại.
- Response giữ field `token` (access) như cũ, thêm `refresh_token`, `refresh_expires_at`.

Trạng thái DB sau login:

| token_hash | family_id | rotated_at | revoked_at | expires_at | family_expires_at |
|---|---|---|---|---|---|
| h(RT1) | F | NULL | NULL | now+30d | now+90d |

---

## 4. Luồng gọi API bình thường (không đụng refresh token)

```
FE ── Authorization: Bearer <access> ──► Protected middleware
                                          │ parse chữ ký + exp
                                          │ SELECT user by id
                                          │ user.active?
                                          │ claims.pwd_changed_at == user.pwd_changed_at?
                                          ▼
                                        Handler
```

- Access token sống theo `jwt.expiry_minutes` (hiện **1440 phút**, sẽ hạ 15 phút sau khi FE xong).
- Không có refresh token nào bị đụng tới trong luồng này.
- Đổi pass/khoá acc có hiệu lực ngay ở request kế tiếp vì middleware query DB mỗi lần.

---

## 5. Luồng refresh thành công

```
FE                          API                         PostgreSQL
 │ POST /auth/refresh        │                              │
 │ { refresh_token: RT1 }    │                              │
 ├──────────────────────────►│                              │
 │                           │ BEGIN                        │
 │                           │ SELECT ... WHERE hash=h(RT1) │
 │                           │   FOR UPDATE ───────────────►│
 │                           │ kiểm tra trạng thái          │
 │                           │ UPDATE RT1 rotated_at=now ──►│
 │                           │ INSERT RT2 (cùng family) ───►│
 │                           │ COMMIT                       │
 │                           │ SELECT user, check active    │
 │                           │ ký access mới                │
 │◄──────────────────────────┤ { token, refresh_token: RT2 }│
```

Trạng thái DB sau refresh:

| token_hash | family_id | rotated_at | revoked_at | ghi chú |
|---|---|---|---|---|
| h(RT1) | F | **t1** | NULL | đã dùng, không xoay lại được |
| h(RT2) | F | NULL | NULL | token mới, hạn mới (≤ family_expires_at) |

Quy tắc:

- **Chỉ một refresh token sống tại một thời điểm** trong một family.
- Token mới có hạn `now + refresh_expiry_days` nhưng **không vượt** `family_expires_at`.
- Response trả **cả cặp mới**; client phải thay cả hai, không giữ token cũ.
- Nếu `IssueToken` lỗi (gần như không xảy ra với HMAC) → 500; token đã xoay nhưng chưa trả về,
  client dùng token cũ sẽ bị coi là reuse ở lần sau. Chấp nhận vì đây là lỗi cấu hình nghiêm trọng.

---

## 6. Luồng refresh khi token đã bị xoay (reuse detection)

Đây là luồng quan trọng nhất — mô phỏng token bị copy.

```
Kẻ tấn công                 API                         PostgreSQL
 │ POST /auth/refresh        │                              │
 │ { refresh_token: RT1 }    │                              │
 ├──────────────────────────►│ SELECT RT1 FOR UPDATE ──────►│
 │                           │ rotated_at != NULL           │
 │                           │ → ErrRefreshReuse            │
 │                           │ (transaction rollback)       │
 │                           │                              │
 │                           │ UPDATE toàn bộ family F      │
 │                           │   SET revoked_at=now, ──────►│
 │                           │   reason='reuse_detected'    │
 │                           │ log Warn: user/family/ip     │
 │◄──────────────────────────┤ 401 "phiên không hợp lệ"     │
 │                           │                              │
 │  (user thật cũng bị đăng xuất ở request refresh kế tiếp)│
```

Trạng thái DB sau reuse:

| token_hash | family_id | rotated_at | revoked_at | revoke_reason |
|---|---|---|---|---|
| h(RT1) | F | t1 | **t2** | **reuse_detected** |
| h(RT2) | F | NULL | **t2** | **reuse_detected** |

Vì sao thu hồi cả family:

- Không phân biệt được ai là kẻ trộm (cả hai đều cầm token).
- Giữ phiên = kẻ trộm giữ quyền vô hạn; thu hồi = cả hai phải login lại, kẻ trộm mất quyền ngay.
- Log `auth: refresh token bị dùng lại, đã thu hồi cả phiên` + `user_id`/`family_id`/`ip` là tín hiệu
  cần theo dõi (nếu xuất hiện nhiều → kiểm tra FE refresh song song hoặc có tấn công thật).

Lưu ý: token **hết hạn** hoặc **family quá hạn** chỉ trả 401, **không** revoke family — chúng
vốn đã chết, không có gì để bảo vệ.

---

## 7. Luồng refresh khi user bị khoá / đã bị xoá

```
Rotate thành công (token hợp lệ, chưa xoay)
  → SELECT user theo old.user_id
     ├─ không thấy (đã xoá)  → thu hồi toàn bộ token của user → 401
     └─ active = false        → thu hồi toàn bộ token của user → 403 "tài khoản đã bị khóa"
```

Điểm cần biết: token đã bị **xoay trước** khi kiểm tra user (vì cần đọc token mới biết `user_id`).
Nếu user bị khoá, token vừa xoay cũng nằm trong diện bị thu hồi — không có khe hở.

---

## 8. Luồng logout (một thiết bị)

```
FE ── POST /auth/logout { refresh_token: RT2 } ──► API (public, không cần Bearer)
                                                     │ tìm family chứa h(RT2)
                                                     │ UPDATE family SET revoked_at=now,
                                                     │   reason='logout'
                                                     │◄── 200 { revoked: n }
```

- **Idempotent**: token không tồn tại/đã chết → `revoked: 0`, vẫn 200 (không cho dò token).
- Gửi token đã xoay (RT1) cũng tìm ra family và thu hồi đúng phiên — hữu ích khi FE lỡ giữ token cũ.
- **Access token hiện tại vẫn dùng được tới hết hạn** (tối đa bằng `expiry_minutes`). Muốn chết
  ngay phải thêm kiểm tra session mỗi request (`sid` claim) — xem P1 trong tài liệu thiết kế.
- Các family khác của cùng user **không** bị ảnh hưởng.

---

## 9. Luồng logout-all (mọi thiết bị)

```
FE ── POST /auth/logout-all (Bearer access) ──► API
                                                │ user_id lấy từ JWT (không nhận từ body)
                                                │ UPDATE toàn bộ token của user
                                                │   revoked_at=now, reason='logout'
                                                │◄── 200 { revoked: n }
```

Dùng khi nghi ngờ lộ token trên thiết bị không còn kiểm soát. Access token của các thiết bị khác
vẫn sống tới hết hạn (cùng lý do như trên).

---

## 10. Luồng đổi mật khẩu

```
POST /auth/change-password (Bearer access)
  → verify mật khẩu cũ
  → UPDATE password + password_changed_at = now
  → UPDATE toàn bộ refresh token của user (reason='password_changed')
  → response
```

Hai lớp thu hồi:

1. `password_changed_at` mới làm **mọi access token cũ rớt ngay** ở middleware.
2. `RevokeAllByUser` làm **mọi refresh token cũ không đổi được vé mới**.

Kết quả: đổi mật khẩu = đăng xuất toàn bộ thiết bị, kể cả thiết bị đang dùng.

---

## 11. Đua 2 tab cùng refresh (điểm dễ sai nhất)

```
Tab A                    Tab B                    API/DB
  │ refresh(RT1)           │                        │
  ├────────────────────────┼───────────────────────►│ lock RT1 (A thắng)
  │                        │ refresh(RT1)           │
  │                        ├───────────────────────►│ chờ lock
  │                        │                        │ A: rotated_at=t1, INSERT RT2, COMMIT
  │◄───────────────────────┼────────────────────────┤ A nhận RT2
  │                        │                        │ B đọc RT1: rotated_at != NULL
  │                        │                        │ → REUSE → revoke cả family
  │                        │◄───────────────────────┤ B nhận 401
  │ dùng RT2 → 401 (family đã bị thu hồi)           │
```

`FOR UPDATE` đảm bảo **chỉ một** request xoay được, nhưng request còn lại sẽ kích hoạt reuse.
Đây là lý do FE **bắt buộc** single-flight:

- Trong 1 tab: giữ một promise refresh dùng chung.
- Giữa các tab: `BroadcastChannel` hoặc lock qua `localStorage` để chỉ 1 tab refresh.
- Nếu không làm, user mở 2 tab sẽ bị đăng xuất khi access token hết hạn.

---

## 12. Ma trận lỗi của `/auth/refresh`

| Điều kiện đầu vào | Repo trả | Service xử lý | HTTP | Thay đổi DB |
|---|---|---|---|---|
| Hash không tồn tại | `ErrRefreshNotFound` | 401 chung | 401 | Không |
| Token đã bị thu hồi | `ErrRefreshRevoked` | 401 chung | 401 | Không |
| Token quá `expires_at` | `ErrRefreshExpired` | 401 chung | 401 | Không |
| Family quá `family_expires_at` | `ErrRefreshExpired` | 401 chung | 401 | Không |
| Token đã xoay (reuse) | `ErrRefreshReuse` | Thu hồi family + log | 401 | **Revoke cả family** |
| Hợp lệ nhưng user đã xoá | — | Thu hồi mọi token user | 401 | Revoke all |
| Hợp lệ nhưng user bị khoá | — | Thu hồi mọi token user | 403 | Revoke all |
| Hợp lệ hoàn toàn | — | Ký access mới | 200 | Rotate + insert |
| Lỗi DB | lỗi gốc | 500 (Render che chi tiết) | 500 | Rollback |

Mọi lỗi xác thực dùng **cùng một message** để người dò token không biết mình sai ở bước nào.

---

## 13. Hợp đồng phía FE

```ts
// single-flight trong tab
let refreshing: Promise<TokenPair> | null = null;

async function getFreshTokens(): Promise<TokenPair> {
  if (!refreshing) {
    refreshing = api
      .post("/api/v1/auth/refresh", { refresh_token: store.refreshToken })
      .then((pair) => {
        store.setTokens(pair); // thay CẢ access và refresh
        return pair;
      })
      .finally(() => {
        refreshing = null;
      });
  }
  return refreshing;
}

// khi API trả 401 do access hết hạn
async function callWithRetry(fn) {
  try {
    return await fn(store.accessToken);
  } catch (e) {
    if (e.status !== 401) throw e;
    const pair = await getFreshTokens();
    return fn(pair.token); // retry đúng 1 lần
  }
}
```

Quy tắc:

- Refresh trả 401 → xoá token, về trang đăng nhập, **không retry vô hạn**.
- Không gọi refresh cho 401 của `/auth/login` hoặc chính `/auth/refresh`.
- Luôn thay cả cặp token; không tái sử dụng refresh token cũ.
- Giữa các tab: `BroadcastChannel`/localStorage lock (xem mục 11).

---

## 14. Ví dụ curl (thay giá trị thật khi chạy)

```bash
BASE=http://localhost:9001/api/v1

# 1) Login
curl -s -X POST $BASE/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"<user>","password":"<pass>"}'
# → { "token": "<access1>", "refresh_token": "<RT1>", "expires_at": "...", ... }

# 2) Refresh (RT1 dùng 1 lần)
curl -s -X POST $BASE/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{"refresh_token":"<RT1>"}'
# → { "token": "<access2>", "refresh_token": "<RT2>", ... }

# 3) Dùng lại RT1 → 401, đồng thời RT2 cũng chết (reuse)
curl -s -X POST $BASE/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{"refresh_token":"<RT1>"}'

# 4) Logout bằng RT2 (dù RT2 đã bị thu hồi do reuse, vẫn idempotent)
curl -s -X POST $BASE/auth/logout \
  -H "Content-Type: application/json" \
  -d '{"refresh_token":"<RT2>"}'

# 5) Logout mọi thiết bị
curl -s -X POST $BASE/auth/logout-all \
  -H "Authorization: Bearer <access2>"
```

---

## 15. Vận hành — thứ cần theo dõi

| Log / dấu hiệu | Ý nghĩa | Hành động |
|---|---|---|
| `auth: refresh token bị dùng lại, đã thu hồi cả phiên` | Có reuse: token bị copy **hoặc** FE refresh song song | Xem `ip`/`family_id`; kiểm tra FE single-flight |
| `auth: thu hồi family sau reuse thất bại` | Lỗi DB khi revoke | Kiểm tra DB; token có thể còn sống tới hạn |
| `auth: thu hồi refresh token sau đổi mật khẩu thất bại` | Revoke lỗi nhưng pass đã đổi | Access token vẫn chết nhờ `password_changed_at`; chạy lại logout-all |
| `auth: dọn refresh token hết hạn` | Janitor chạy | Bình thường |
| Bảng `refresh_tokens` tăng nhanh | Nhiều login hoặc janitor không chạy | Kiểm tra worker + `purge-refresh-tokens` |

Không bao giờ log token gốc; log đã gắn `user_id`, `family_id`, `ip`, `trace_id`.

---

## 16. Liên kết

- Thiết kế + lý do + quyết định: `technical_debt/AUTH_REFRESH_TOKEN.md`
- Bảng + repository: `src/domain/refresh_token.go`,
  `src/infrastructure/repository/refresh_token_repository.go`
- Nghiệp vụ: `src/controller/services/user_service.go`
- Endpoint: `src/controller/apis/user_controller.go`, `src/controller/routers/router.go`
- Dọn token: `src/controller/services/refresh_token_janitor.go`, `cmd/worker/main.go`
