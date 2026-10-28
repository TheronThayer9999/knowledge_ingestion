# Đợt 28 — Lộ provider/model lên UI (Nấc 1) + chọn model trong chat

Ngày: 2026-10-28 · Trạng thái: xong (nấc 1 + nấc 2) · Nối: Đợt 8 (CLI/env chọn provider), Đợt 25 (phiên chat), Đợt 27 (chip phạm vi/suy luận)

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

## 8. Kết quả Nấc 2 — provider là POPUP, key vào DB, có load model

Yêu cầu mới: "chọn provider ở config, có API, add API key, load model, model mặc định theo config, và provider là một popup". Chốt (người dùng xác nhận): **bảng DB riêng, config làm mặc định** và **làm popup provider trước**.

### 8.1 Quyết định

- **Khai báo provider vẫn ở `configs/config.json`** (slug/type/base_url). Bảng mới
  `llm_provider_settings` chỉ lưu **override**: `api_key_cipher`, `model`,
  `models` (kết quả load). Merge `MergeProviderSetting(pcfg, row)` — DB thắng
  config từng field, row không có thì config giữ nguyên; dùng chung cho lúc dựng
  agent và lúc rebuild sau khi admin lưu key → restart không mất hiệu lực.
- **Key mã hoá AES-256-GCM** (`common/utils/secret.go`, khoá suy từ JWT secret +
  namespace) — DB lộ cũng không đọc được plaintext. `config.local.json` (giữ key
  thật) vẫn gitignore.
- **API ghi chỉ admin** (`RequireRole(RoleAdmin)` từng route): `PUT|DELETE
  /llm/providers/:slug/key`, `POST /llm/providers/:slug/models` (gọi
  OpenAI-compatible `GET {base}/models` hoặc Ollama `/api/tags`), `PUT
  /llm/providers/:slug/model`. `GET /llm/options` vẫn cho mọi user và **không
  bao giờ trả `api_key`**; `base_url`/`has_key`/`key_source`/`config_model` chỉ
  admin.
- **UI là popup** (`LlmProvidersDialog`): Settings chỉ còn card mô tả + nút mở;
  popup liệt kê provider dạng accordion, mở sẵn provider mặc định, mỗi dòng có ô
  key (password), nút Lưu/Xoá key, select model + "Tải model" + "Dùng model
  này". Trang Settings **không gọi API** nữa — user thường tốn 0 request.
- Model mặc định = `config.models[0]`/`config.model` khi DB chưa chọn; chọn
  trong popup ghi `model` vào row và rebuild agent ngay (mutex trong
  `ProviderAgents`).

### 8.2 Kiểm chứng E2E (port 9001, DB thật)

- Lưu key qua `PUT .../key` → DB có cipher (58 ký tự, không plaintext),
  `key_source` đổi config → database; `DELETE` xoá cipher và rơi về config.
- Key DB sai → lượt chat trả 502 kèm lỗi 401 của provider (chứng minh DB thắng
  config).
- `POST .../models` với 9router trả 503 model và lưu vào DB; `GET /llm/options`
  thấy danh sách mới.
- Token user thường: `PUT/DELETE/POST` provider đều 403; options không có
  `api_key`, `base_url`, `has_key`. Không response nào chứa `api_key`.
- Dọn row test sau khi kiểm xong.

## 9. Phụ trong đợt — đính kèm tệp ở màn AI chat

Yêu cầu: "màn AI chat chưa có nút upload file để user upload trực tiếp file để
hỏi AI".

- **Trích ĐỒNG BỘ, không nạp kho**: `POST /api/v1/chat/attachments` (multipart
  `file`, trần 15 MB) chạy đúng registry `extractor` của pipeline → trả
  `{name, text, chars}`. FE gửi lại trong `ChatRequest.attachment` của **đúng
  lượt đó**; `withAttachment` chèn khối `TỆP ĐÍNH KÈM` vào prompt, dặn model
  không gắn `[^n]` cho tệp. Không qua worker/Qdrant nên trả lời được ngay, và
  tệp không sống sang lượt sau (bản lưu phiên giữ câu hỏi gốc).
- Chốt chặn phía server: tên tệp gột newline + cắt `MAX_FILENAME_LEN`, text cắt
  `CHAT_ATTACHMENT_MAX_CHARS` (60000 rune) ở cả service lẫn `withAttachment`;
  tệp rỗng/không đọc được → 400 kèm lý do; PDF scan (API không bật OCR) báo rõ
  "hãy nạp vào kho tri thức".
- FE: nút `attach_file` trong composer (input file ẩn), chip tên tệp + nút bỏ,
  đang trích thì hiện "Đang đọc tệp…", lỗi trả tệp về composer để Gửi lại; lượt
  user trong transcript hiện tên tệp.
- E2E: txt → text đúng; `.zip` → 400 kèm danh sách đuôi hỗ trợ; txt rỗng → 400;
  lượt chat kèm tệp trả đúng "doanh thu tăng 12%" và `cites: []` (không bịa
  nguồn cho tệp).
- Unit: `chat_attachment_service_test.go` (trích, định dạng lạ, tệp rỗng, cắt
  theo rune, gột tên) và `chat_attachment_test.go` (khối prompt, no-op, cắt
  text client, gột tên). FE: 3 test service (multipart, thiếu `chars`, body
  `send` có attachment).

