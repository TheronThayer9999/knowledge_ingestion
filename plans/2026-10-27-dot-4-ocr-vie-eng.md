# Đợt 4 — OCR song ngữ `vie+eng` + vá lại tiêu chí gọi VLM

## Bối cảnh

- Máy dev đã cài **3 gói** tessdata: `eng`, `osd`, `vie` (xem `_tesseract.md`), nhưng config
  vẫn để `ocr.lang = "vie"`.
- Hệ quả: văn bản **tiếng Anh** (hoặc tên riêng/trích dẫn tiếng Anh nằm trong văn bản Việt) bị
  ép qua model tiếng Việt — model không có từ điển tiếng Anh nên đọc sai/không dấu.
  `tesseract -l vie+eng` cho phép engine **chọn model tốt hơn cho từng từ**, đúng ca "văn bản
  hành chính Việt có thuật ngữ tiếng Anh".

## Chốt lại ranh giới với VLM (quan trọng hơn phần config)

Giới hạn thật của Tesseract là **chữ viết tay**, **không phải ngôn ngữ**:

| Loại đầu vào | Đường xử lý | Vì sao |
|---|---|---|
| Chữ **in** tiếng Việt | `pdftotext` → `tesseract -l vie` | đúng sẵn, rẻ, tất định |
| Chữ **in** tiếng Anh / hỗn hợp | `tesseract -l vie+eng` | đọc tốt, **không** phải lý do gọi VLM |
| Bảng nhiều cột | L1 (đã có `layout_score`) → L2 `pdftohtml -xml` | tất định, không tốn token |
| **Chữ viết tay**, chữ ký, ghi chú tay | **VLM (L3)** | Tesseract học trên chữ in ⇒ gần như vô dụng; đây mới là ca đáng trả tiền token |

⇒ `RAG_RESEARCH.md` §6.12-d thêm **tiêu chí 0: chữ viết tay**, và ghi rõ "tài liệu tiếng Anh"
**không** là lý do gọi VLM.

## Thay đổi

| Nơi | Việc |
|---|---|
| `configs/config.json` + `configs/config.local.json` | `ocr.lang: "vie+eng"` |
| `src/config/config.go` | comment: default khi config trống vẫn là `vie` (máy chưa cài `eng` sẽ chết ngay), chọn `vie+eng` là quyết định của config |
| `src/common/extractor/ocr.go` | `ListLangs(ctx, binary)` (`tesseract --list-langs`) + `MissingLangs(available, lang)` (hàm thuần) |
| `src/loader/worker.go` | `fx.Invoke`: đọc danh sách lang lúc boot, thiếu gói nào thì **warn** kèm danh sách có sẵn |
| `_tesseract.md` | mục "Đa ngôn ngữ (vie+eng)" + giới hạn chữ viết tay |
| `RAG_RESEARCH.md` §6.12-d | tiêu chí 0: chữ viết tay |

## Vì sao phải validate lúc boot

Thiếu `traineddata` **không** phải "kém chính xác" — tesseract exit `!= 0` cho **mọi** trang,
nên job scan đi hết 10 lần retry rồi `failed` mà log chỉ nói "tesseract lỗi". Warn lúc boot
(và **không** chặn boot, vì vẫn có thể chỉ dùng PDF có text layer) là cách rẻ nhất để người
vận hành biết ngay. Cùng pattern với warn `render_binary` đang có.

## Rủi ro đã chấp nhận

- `vie+eng` chậm hơn ~1.5–2×/trang vì nạp 2 model. Chỉ chạy trên trang không có text layer nên
  chấp nhận; nếu thành vấn đề thì tách theo trang (nhiều ký tự ASCII → `eng`).
- Không tự đổi default trong code sang `vie+eng`: máy chưa cài `eng` sẽ hỏng ngay từ trang scan
  đầu tiên. Quyền chọn thuộc config.
- `ListLangs` gọi binary lúc boot: nếu binary trỏ sai đường dẫn thì mất ~10s timeout (chỉ worker
  boot, không phải mỗi request).

## Nghiệm thu

- [x] `TestMissingLangs` (offline, 6 ca) + `TestListLangs` chạy với engine thật (0.06s, parse đúng `eng/osd/vie`).
- [x] `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), `go test ./...` xanh.
- [x] Boot worker thật với `config.local.json`: `INFO loader/worker.go ocr: đủ traineddata {"lang": "vie+eng", "available": ["eng","osd","vie"]}` — không cảnh báo sai.

## Kết quả

- `vie+eng` đã bật ở `configs/config.json` + `configs/config.local.json`; default trong code vẫn là `vie` (máy
  thiếu gói `eng` sẽ làm tesseract exit `!= 0` cho mọi trang, nên default không được đổi theo máy dev).
- Validate lúc boot là **warn-only** — không chặn boot, cùng pattern với warn `render_binary`.
- Điểm chốt đã ghi vào `RAG_RESEARCH.md` §6.12-d + `_tesseract.md`: **chữ viết tay mới là lý do gọi VLM**;
  chữ in (Việt/Anh) và trang có bảng đi đường tất định.

## Commit

1. `docs(rag): add batch 4 plan for bilingual OCR`
2. `feat(ocr): list installed traineddata and warn when missing`
3. `chore(config): use vie+eng for OCR and record handwriting limit`
