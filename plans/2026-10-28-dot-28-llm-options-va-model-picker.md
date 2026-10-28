# Đợt 28 — Lộ provider/model lên UI (Nấc 1) + chọn model trong chat

Ngày: 2026-10-28 · Trạng thái: đang code · Nối: Đợt 8 (CLI/env chọn provider), Đợt 25 (phiên chat), Đợt 27 (chip phạm vi/suy luận)

## 1. Yêu cầu

Người dùng muốn xem cách các dự án mã nguồn mở thiết kế phần **provider/model trên UI**, vì hiện tại mọi thứ nằm trong `configs/config.json`. Sau khi đối chiếu Open WebUI, LibreChat, LobeChat, Dify, AnythingLLM, Cherry Studio, chốt làm **Nấc 1**:

- Phản chiếu cấu hình provider ra UI (không đưa API key lên trình duyệt).
- Cho người dùng chọn provider/model khi chat; mặc định vẫn là cấu hình.
- Admin xem được danh sách kết nối + trạng thái key/khả dụng trong Settings.

Ràng buộc: **không đổi cách lưu key** (vẫn ở config), **không thêm bảng DB**, không đụng logic retrieval/prompt/citation.

## 2. Quyết định

- **Đổi provider/model theo TỪNG LƯỢT**, không khoá theo phiên: backend đã nhận `provider`/`model` mỗi request, conversation/message đã lưu per-message; Open WebUI/LibreChat cũng cho đổi giữa hội thoại. Khác với `scope`/`thinking` (bị khoá vì backend chỉ đọc khi request để trống).
- **Không cho user nhập API key riêng** ở Nấc 1 (khác LibreChat `user_provided`) — key vẫn do admin cấu hình file.
- **Một endpoint đọc duy nhất**: `GET /api/v1/llm/options` (đăng nhập). Trả về danh sách provider đã cấu hình + model gợi ý + trạng thái; **không bao giờ trả `api_key`**. `base_url` và `has_key` chỉ trả cho admin (`role >= 1`).
- **Config thêm field optional** `label`, `models` (không đổi precedence flag > env > file > default). `models` rỗng thì lấy đúng `model` hiện có — config cũ chạy nguyên.
- **`ChatResponse` thêm `provider`/`model`** (additive) để FE gắn badge model cho từng câu trả lời mới; message mở lại từ DB đã có sẵn 2 cột này.
- Provider `available=false` (thiếu key nên loader không dựng agent) vẫn hiện trong Settings cho admin, nhưng **không** xuất hiện trong menu chọn của chat.

## 3. API

`GET /api/v1/llm/options` — Bearer. Không tham số.

```json
{
  "default_provider": "9router",
  "default_model": "kc/nvidia/nemotron-3.5-super-vl:free",
  "providers": [
    {
      "slug": "9router",
      "label": "9Router",
      "type": "openai",
      "default_model": "kc/nvidia/nemotron-3.5-super-vl:free",
      "models": ["kc/nvidia/nemotron-3.5-super-vl:free"],
      "available": true,
      "thinking": true,
      "is_default": true,
      "base_url": "http://localhost:20128/v1",   // chỉ admin
      "has_key": true                            // chỉ admin
    }
  ]
}
```

- Sắp theo `slug` để kết quả tất định (config là map).
- `models`: `config.models` đã trim/dedup; rỗng thì `[config.model]` nếu có.
- `available`: agent đã dựng trong `ProviderAgents.Agents` (key hợp lệ); `thinking`: có trong `ThinkingAgents`.
- `is_default`: slug trùng `llms.default_provider` (sau khi lowercase/trim).

`POST /api/v1/chat` và `POST /api/v1/conversations` đã nhận `provider`/`model` — FE bắt đầu gửi. Response chat thêm:

```json
{ "provider": "9router", "model": "kc/nvidia/nemotron-3.5-super-vl:free", "...": "..." }
```

## 4. UI

**Chat — chip "Mô hình"** trong `.card-bar`, cạnh chip phạm vi/suy luận:

```
[☰] [Phạm vi: Toàn bộ kho ▾] [Suy luận sâu: Tắt] [Mô hình: 9Router · nemotron ▾] [Phiên mới]
```

- Item đầu: **"Theo cấu hình"** (provider/model rỗng) — hành vi mặc định không đổi.
- Sau đó nhóm theo provider (heading không bấm được), mỗi model là một item; provider chỉ có 1 model vẫn hiện đúng model đó.
- Chọn xong: lưu vào state trang, gửi kèm lượt kế tiếp và lần tạo phiên; **không** khoá khi phiên đã có tin.
- Mở phiên cũ: khôi phục provider/model của phiên (`Conversation.provider/model`); phiên lưu rỗng = "Theo cấu hình".
- "Phiên mới" giữ lựa chọn hiện tại (coi như preference), chỉ reset scope/thinking như cũ.
- Lỗi tải options: ẩn chip, chat vẫn chạy bằng mặc định (không toast làm phiền).

**Badge model** dưới mỗi câu trả lời assistant: `provider · model` (muted, xs). Lượt mở lại lấy từ `SavedMessage`; lượt mới lấy từ `ChatAnswer` (response vừa thêm field).

**Settings — card "Nhà cung cấp AI"** chỉ admin thấy:

- Dòng mỗi provider: chấm trạng thái (khả dụng/không), label + slug, protocol, model mặc định, base URL, "Đã có key"/"Chưa có key", nhãn "Mặc định".
- Ghi chú: key đọc từ `configs/config.json`; sửa file rồi khởi động lại. **Không có ô nhập key** ở Nấc 1.
- User thường không thấy card này (dùng `AuthService.isAdmin`).

## 5. File

Mới:
- `src/controller/dtos/llm_dto.go` + test JSON (không lộ key)
- `src/agent/usecase/llm_options_service.go` + `_test.go`
- `src/controller/apis/llm_api.go`
- `knowledge_ingestion_fe/src/app/core/llm/llm.models.ts|llm.service.ts`
- (FE) test cho chip model

Sửa:
- `src/config/config.go` (`Label`, `Models`)
- `src/config/config.local.json` (label cho 5 provider; models để trống)
- `src/agent/port/ports.go` (`ILlmOptions`)
- `src/controller/dtos/chat_dto.go` (provider/model response)
- `src/agent/usecase/chat_service.go` (chốt provider slug + trả response)
- `src/controller/routers/router.go`, `src/loader/loader.go`
- `knowledge_ingestion_fe/src/app/core/chat/chat.models.ts|chat.service.ts`
- `pages/chat/chat.ts|html|scss`, `chat-context-chips.ts|html|scss`
- `pages/settings/settings.ts|html|scss`
- `shared/i18n/vi.ts|en.ts`
- `docs/` (swag)

## 6. Kiểm chứng

- BE: `go build ./... && go vet ./... && golangci-lint run ./... && go test ./...`; `swag init -g cmd/apis/main.go`.
- Unit: options builder (admin vs user, available/thinking/default, models fallback, JSON không có `api_key`, user không có `base_url`/`has_key`); chat response có provider/model đã resolve.
- FE: `npm run lint`, `npm test -- --watch=false`, `npm run build`.
- E2E thật: login → `GET /llm/options` bằng token user và admin; chat chọn provider khác default trả đúng `provider/model`; UI chọn model → gửi → badge đúng; mở lại phiên giữ lựa chọn; Settings admin thấy card, user thường không.
- Test tay: menu model trên mobile drawer, bàn phím, badge không làm vỡ bubble dài.

## 7. Không làm (để Nấc 2)

- Bảng `llm_providers` + mã hoá key + CRUD trên UI + precedence DB > env/file + audit.
- Nút Test connection / Fetch models từ provider.
- User tự nhập API key riêng.
- Marketplace/capability registry kiểu Dify.
