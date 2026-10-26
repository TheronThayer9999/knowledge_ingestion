# Đợt 23 — Chat FE: footnote bấm được, NGUỒN TRÍCH DẪN gập mở, hết chồng icon tune

## Bối cảnh (lỗi anh chụp màn hình)

BE đã chốt khuôn `[^n]` ở Đợt 22 (`6235565`), nhưng FE vẫn in nguyên văn vì
`marked` core **không có footnote** (`knowledge_ingestion_fe/src/app/shared/markdown/markdown.pipe.ts`
chỉ `marked.parse(value, {gfm, breaks})`). Thêm hai lỗi hiển thị nữa:

1. Khối **NGUỒN TRÍCH DẪN** là `div.cites` thường trực (`chat.html:63-88`) → không đóng/mở được,
   câu trả lời dài bị một dãy nguồn chiếm chỗ cuối bong bóng.
2. **Số trích dẫn lệch với danh sách**: không có liên kết nào giữa `[^3]` trong đáp án và mục
   thứ 3 trong `NGUỒN TRÍCH DẪN`.
3. Nút **tune** `position:absolute; top/right: --space-2` (`chat.scss:87-95`) nổi đè ngay góc
   trên-phải card, đúng chỗ bong bóng **user** (canh phải) đi lên → icon đè chữ.

## Mục tiêu

- `[^n]` render thành **chip số superscript bấm được**; bấm vào thì mở khối nguồn + cuộn tới +
  nháy sáng đúng mục `n`.
- **NGUỒN TRÍCH DẪN** thành `details/summary` mặc định **gập**, tiêu đề kèm số nguồn, theo đúng
  khuôn `details.reasoning` đã có trong trang (`chat.html:51-60`).
- Icon tune **không bao giờ nằm đè nội dung** nữa.
- Câu trả lời tiếp tục render bằng thư viện (`marked`) — không tự viết parser markdown.

## Ngoài phạm vi (để Đợt 24 trở đi)

- Lịch sử chat lưu DB + sidebar phiên kiểu Qwen (Đợt 24) — đợt này **không** đổi vị trí panel
  tiện ích sang trái, chỉ sửa chỗ đè nút.
- Streaming, chia sẻ/audit hội thoại.
- Đổi layout/khuôn khổ của danh sách cite (đã ổn, chỉ bọc vào `details`).

## Quyết định thiết kế

| Vấn đề | Chọn | Vì sao |
|---|---|---|
| Footnote | Tự viết **marked extension** (`use({extensions})`, inline tokenizer) thay vì kéo `markdown-it-footnote` | Extension API là đường chính chủ của marked; footnote chuẩn lại **đòi** khối `[^n]: định nghĩa` + tự dựng mục Footnote cuối bài, trong khi định nghĩa của mình nằm ở `cites` (API trả) — dùng lib sẽ nhân đôi phần nguồn. |
| Truyền số `n` cho handler | Đọc từ **`textContent`** của chip, không dùng `data-cite`/`id` | `id` và `data-*` **không** nằm trong allowlist sanitizer của Angular (`HTML_ATTRS`/`ARIA_ATTRS` trong `@angular/core`), còn `class`/`role`/`tabindex`/`title`/`aria-*` thì có. |
| Trạng thái gập | **Thuần DOM** qua `details.open`, không đưa vào signal | `@for` track theo `$index`/`cite.n` nên phần tử được tái sử dụng, trạng thái giữ tự nhiên; ràng buộc `[open]` hai chiều với `(toggle)` chỉ tổ có vòng lặp ghi-đọc. |
| Style chip | Đặt ở **`src/styles/styles.scss`** (toàn cục), không phải `chat.scss` | HTML chèn qua `[innerHTML]` không mang attribute scope của component — chính file đã ghi chú điều này ở dòng 63. |
| Instance marked | `new Marked({gfm, breaks})` + cache theo label, thay vì `marked.use` toàn cục | `marked.use` sửa singleton của cả app; label i18n khác nhau cần parser khác nhau, cache Map 2 phần tử là đủ. |
| Keyboard | `role="button" tabindex="0"` trên `sup` + handler `keydown` (Enter/Space) | Giữ nguyên markup footnote (sup) mà vẫn bàn phím được; **nếu spec chứng minh sanitizer cắt thuộc tính này** thì fallback sang `<button class="md-cite-ref">` và ghi lại vào plan. |

## File sẽ chạm

| File | Việc |
|---|---|
| `src/app/shared/markdown/citation-ref.extension.ts` | **Mới** — tokenizer inline `/^\[\^(\d{1,3})\]/` → `<sup class="md-cite-ref" role="button" tabindex="0" aria-label="…">n</sup>`; escape label trước khi nhét vào attribute |
| `src/app/shared/markdown/markdown.pipe.ts` | Nhận thêm arg `citeLabel`, dùng `Marked` riêng có extension; `transform` vẫn trả `string` để Angular tự sanitize (không bypass) |
| `src/app/shared/markdown/markdown.pipe.spec.ts` | **Mới** — bằng chứng thật: `[^3]` ra chip; `[*3]`/`[3]` **không** ra chip; `[^1][^2]` ra 2 chip; **sanitizer có giữ `role`/`tabindex`/`class` không** (quyết định fallback) |
| `src/app/pages/chat/chat.html` | Bọc `.cites` bằng `details` + `summary` (đếm nguồn); thêm `(click)`/`(keydown)` lên `.markdown-body`; truyền label vào pipe; chuyển nút tune vào `.card-bar` |
| `src/app/pages/chat/chat.ts` | `onCiteRefClick`/`onCiteRefKey` → `jumpToCite(index, n)` (mở `details`, `scrollIntoView`, thêm `is-flash`, gỡ khi `animationend`); tìm bubble theo `$index` qua `#scroller` |
| `src/app/pages/chat/chat.scss` | `.card-bar` (thay `.panel-toggle` absolute); style `summary.cites-title` + `.is-flash`; giữ nguyên phần `.cite` đã có |
| `src/styles/styles.scss` | `.markdown-body .md-cite-ref` (chip) — dùng token, `em` được phép trong khối markdown (`token-usage.spec.ts`) |
| `src/app/shared/i18n/vi.ts` + `en.ts` | Thêm `CHAT_CITE_JUMP` (canonical `VI_ENTRIES` → thiếu bên `en` là biên dịch gãy, đúng ý) |

## Tiêu chí nghiệm thu

- Đáp án có `[^2]` hiển thị số 2 dạng superscript **bấm được**, bấm là khối nguồn mở ra và mục
  số 2 sáng lên rồi tắt sau animation; bấm bằng Enter/Space cũng được như chuột.
- `NGUỒN TRÍCH DẪN (4)` gập lại theo mặc định, bấm summary để mở/đóng; đóng/mở không làm mất vị trí
  cuộn của khung chat.
- Ở bong bóng user ngắn nhất nằm ngay góc trên-phải, **không** có pixel nào của nút tune đè lên chữ.
- Bảng/danh sách/code trong đáp án vẫn render như cũ (không hồi quy markdown).

## Lệnh kiểm tra

```
cd knowledge_ingestion_fe
npm run lint
npm test
npm run build
npx prettier --write <file vừa sửa>   # lint không tự format
```

Phần UI/UX trên browser anh tự kiểm tra (theo ý đã chốt).

## Kết quả

(điền sau khi xong đợt)
