# Đợt 27 — Sidebar quản lý phiên chat + hiệu ứng stream đáp án

Ngày: 2026-10-27 · Trạng thái: kế hoạch (chưa code) · Nối: Đợt 25 (phiên chat DB + sidebar), Đợt 26 (biểu đồ trong đáp án — đã code xong FE `49b6322`)

## 1. Yêu cầu đã nhận

Người dùng đưa 9 nhóm yêu cầu cho phần **quản lý phiên** (sidebar "Trợ lý tri thức"), kèm một yêu cầu riêng: "code đoạn render message làm hiệu ứng stream đi". Ràng buộc xuyên suốt: **giữ nguyên API và logic truy vấn tài liệu, chỉ đổi giao diện + trạng thái phiên**; không đổi hành vi trả lời/trích dẫn của AI.

Các nhóm: (1) tự đặt tiêu đề từ câu hỏi đầu ≤40 ký tự cắt theo ranh giới từ + đổi tên inline; (2) số tin nhắn & thời gian cập nhật ngay khi gửi, nhãn "tin nhắn" thay "lượt", thời gian tương đối; (3) danh sách cuộn dọc, thẻ full-width, tiêu đề ellipsis, nhóm Hôm nay/Hôm qua/7 ngày qua/Cũ hơn, sắp theo hoạt động gần nhất, phiên đang chọn nổi bật; (4) nút sửa/xoá ẩn tới hover/focus (hoặc gom menu ⋯), xoá màu đỏ chỉ trong menu, luôn hiện hộp xác nhận, thao tác được bằng bàn phím; (5) phạm vi + suy luận sâu thành chip có nhãn trên đầu khung chat, khoá trong phiên kèm tooltip "tạo phiên mới để đổi", nút phiên mới cạnh chip; (6) sidebar có empty state gợi ý + câu hỏi mẫu; (7) nút thu gọn/mở rộng có nhãn rõ, dưới 768px sidebar thành overlay trượt từ trái; (8) nút Dừng giữ nguyên, ô nhập khoá mềm + nhắc "Chờ trả lời xong", chấm trạng thái cho phiên đang xử lý; (9) trạng thái sống qua reload, WCAG AA, aria-label, thứ tự Tab.

## 2. Quyết định kiến trúc

- **Không đụng backend.** `POST /conversations` đã nhận `title` (`CreateConversationRequest.Title`, max 120) và `AppendTurn` chỉ điền title khi đang rỗng (`SetTitleIfEmpty`) → FE tự sinh title ngay lúc tạo phiên là đủ, không cần endpoint mới.
- **Tách 2 component con** thay vì nhét thêm vào `chat.ts` (đã 632 dòng) và `chat.scss` (đã 816 dòng, ngân sách `anyComponentStyle` 14kB/16kB tính theo từng component): `app-chat-sessions` (danh sách phiên) và `app-chat-context-chips` (chip phạm vi + suy luận sâu + phiên mới). Chat page vẫn là chủ mọi state thật (services, gửi câu hỏi, xoá/đổi tên).
- **Logic thuần tách riêng để test được không cần DOM**: `session-time.ts` (timeAgo + groupSessions), `stream-reveal.ts` (chia mảnh + bước hoạt cảnh), `clipSessionTitle` (trong `chat.models.ts`).
- **Hiệu ứng stream là hoạt cảnh phía FE**, không phải SSE: backend trả một khối. Đáp án cuối cùng hiển thị vẫn đúng nguyên văn từng ký tự, nên trích dẫn/chart không đổi hành vi.

## 3. Tiêu đề phiên (nhóm 1)

- `clipSessionTitle(question)` trong `core/chat/chat.models.ts`: TrimSpace; ≤40 rune thì giữ nguyên; dài hơn thì cắt tại **dấu cách cuối cùng trước rune 40**, không có dấu cách nào thì cắt cứng ở 40, bỏ dấu câu treo cuối, nối `…`. Đếm theo rune để tiếng Việt không bị xé ký tự.
- `resolveSession(question)` gửi `title: clipSessionTitle(question)` khi tạo phiên lazy.
- Sidebar: `title === '' && messageCount === 0` → `CHAT_UNTITLED` ("Chưa đặt tên"); `title === '' && messageCount > 0` → key mới `CHAT_SESSION_UNTITLED` ("Cuộc trò chuyện không tên") — không còn trạng thái "chưa đặt tên" giả vờ sắp có tên.
- Đổi tên inline: giữ cơ chế Enter lưu / Esc hủy, thêm **autofocus** ô nhập + `aria-describedby` không cần thiết; blur không lưu (như cũ, tránh PATCH ngoài ý muốn).

## 4. Số tin nhắn + thời gian (nhóm 2)

- `touchSession(id, {count, at})` cập nhật row tại chỗ **và đưa lên đầu** danh sách.
- Bấm gửi: bump `+1` (lượt user) + `lastMessageAt = now` ngay trong `tap()` của `ask()` — không đợi đáp án. Đáp án về: bump `+1` nữa rồi `refreshSidebar()` (DB vẫn là nguồn thật chốt lại). Lỗi giữa chừng: bump `-1` để hoàn tác optimistic (dùng cho cả `stop()`).
- `sessionTime` cũ chuyển vào `session-time.ts` dưới tên `timeAgo(date, now, t)`: <1 phút "vừa xong", <60 "N phút trước", <24h "N giờ trước", **đúng hôm qua → "hôm qua"** (key mới `CHAT_TIME_YESTERDAY`), <7 ngày "N ngày trước", còn lại `dd/MM/yyyy`.
- Đổi key `CHAT_SESSION_TURNS` → `CHAT_SESSION_MESSAGES`, text `'{n} tin nhắn'` / `'{n} messages'`.

## 5. Danh sách phiên (nhóm 3)

- `groupSessions(conversations, now)` trả `{key, items}[]` với key `today | yesterday | week | older` theo **ranh giới ngày lịch** (không phải 24h trôi), sắp xếp bản sao theo `lastMessageAt` giảm dần trước khi chia bucket, bucket rỗng bị loại.
- Heading nhóm: `CHAT_GROUP_TODAY/YESTERDAY/WEEK/OLDER` (Hôm nay / Hôm qua / 7 ngày qua / Cũ hơn), kiểu uppercase nhỏ như `sessions-heading` hiện tại.
- Cuộn: `.session-list { overflow-y: auto; overflow-x: hidden; }`, mọi phần tử giữa chuỗi flex có `min-width: 0`, `.session-main` full bề ngang phần còn lại, `.session-title` ellipsis (đã có) — thẻ chiếm đủ ngang sidebar nên không còn cuộn ngang.
- Trạng thái chọn: giữ `accent-soft` + viền trái, nâng viền thành 3px và thêm `font-weight` cho title; `aria-current="true"` như cũ.

## 6. Hành động trên phiên (nhóm 4)

- Gom vào **menu `⋯`** (`MatMenuModule`): item "Đổi tên" (edit) + "Xoá phiên" (delete, `.menu-danger` màu `--color-danger` — đỏ **chỉ trong menu**).
- Nút `⋯` ẩn bằng `opacity: 0` và hiện lại khi `.session:hover`, `.session:focus-within`, `.session.is-active` — **không dùng `visibility/display`** vì nút phải còn tab được; focus bàn phím làm cả hàng sáng lên nên không có nút vô hình.
- Xoá luôn qua `messages.confirm('CHAT_DELETE_TITLE', 'CHAT_DELETE_BODY')`; đổi text body thành đúng yêu cầu: "Xóa cuộc trò chuyện này? Không thể hoàn tác." (title) + body nói hệ quả hiển thị; xác nhận rồi mới gọi DELETE.
- Bàn phím: Tab → Enter/Space mở menu, mũi tên chọn, Esc đóng (MatMenu mặc định); ô đổi tên autofocus, Enter/Esc như mục 3.

## 7. Chip phạm vi + suy luận sâu (nhóm 5)

- `app-chat-context-chips` nằm trong `.card-bar` (đầu khung chat, cạnh nút gập sidebar): chip `"Phạm vi: Toàn bộ kho|<tên thư mục>"` (icon `folder_open`, mở MatMenu chọn trong `categories()`), chip `"Suy luận sâu: Bật|Tắt"` (icon `psychology`, `aria-pressed`), nút `"Phiên mới"` (icon `add`).
- `locked` (phiên đã có tin nhắn) → chip là `<button disabled>` + `matTooltip` key mới `CHAT_CHIP_LOCKED_HINT` = "Đã khoá trong phiên này — tạo phiên mới để đổi phạm vi."; `aria-disabled` vẫn cho focus để đọc tooltip.
- Bỏ `mat-form-field`/`mat-select` phạm vi khỏi sidebar (kèm import `MatSelectModule` trong `chat.ts`), bỏ `.panel-hint` (tooltip của chip đã nói).

## 8. Empty state sidebar (nhóm 6)

- Không có phiên nào (không lỗi, không đang tải) → trong `app-chat-sessions`: đoạn `CHAT_SESSIONS_EMPTY` ngắn gọn + 3 nút gợi ý (dùng lại `CHAT_SUGGEST_*`), phát `askExample(key)` để page gọi `ask()`. Đúng nhóm 6 "gợi ý + ví dụ câu hỏi".

## 9. Thu gọn / drawer (nhóm 7)

- Nút gập ở `.card-bar`: `aria-expanded`, `aria-controls="chat-sessions"`, tooltip/aria-label đổi theo trạng thái — key mới `CHAT_SIDEBAR_SHOW` ("Hiện danh sách cuộc trò chuyện") / `CHAT_SIDEBAR_HIDE` ("Ẩn danh sách cuộc trò chuyện") thay cho `CHAT_PANEL_TOGGLE` mơ hồ (giữ key cũ cho nhãn panel? không — thay hẳn, ít chỗ dùng).
- `@media (max-width: 767px)`: aside thành `position: fixed` lớp phủ rộng `min(88vw, 320px)` trượt từ **trái** (`@keyframes drawer-in`, tôn trọng `prefers-reduced-motion`), kèm `.scrim` (nút thật, aria-label "Đóng danh sách") và Esc đóng (`(keydown)` trên aside). Giữ hành vi <1024px hiện tại cho khoảng 768–1023px.
- Theo sở thích đã ghi: panel nằm **trái** card trên desktop (`order: -1`) — không đổi.

## 10. Đang trả lời (nhóm 8) + hiệu ứng stream

- Bỏ `[disabled]` trên textarea; khi `sending()` hiện `CHAT_WAIT_REPLY_HINT` ("Chờ trả lời xong…") dưới ô nhập (`role="status"`) và viền ô đổi nhẹ (`.composer.is-busy`); Enter khi đang gửi bị `ask()` chặn sẵn nên câu chữ không mất.
- Hàng phiên đang xử lý (`.is-active` khi `sending()`) có `.status-dot` pulse + `role="status"` `aria-label="Đang trả lời…"`.
- `shared/markdown/stream-reveal.ts`:
  - `splitRevealChunks(text)` — regex `\[\^\d+\]|\S+\s*|\s+`: mỗi **từ kèm dấu cách sau** là một mảnh, marker `[^n]` là một mảnh nguyên khối (không bao giờ vẽ nửa marker rồi nhảy chip), khoảng trắng/lỗi xuống dòng không bị nuốt.
  - `perTickFor(total)` — `max(1, ceil(total / 45))` để đáp án dài vẫn kết thúc trong ~1,5–2s ở 30fps.
  - `class StreamReveal` — `step()` trả prefix hiện tại, `isComplete()`, `full()`.
  - `Chat`: đáp án về thì bật `setInterval(33ms)` ghi `revealText`; `displayText(turn, index)` trả `revealText()` cho lượt assistant cuối đang hoạt cảnh, ngược lại `turn.content` (mở phiên cũ = hiện tức thì, không hoạt cảnh lại). `cites` + chart chỉ render khi `revealText() === null` để chip nguồn không trỏ vào chữ chưa hiện. Timer dọn ở complete / `stop()` / đổi phiên / `onDestroy`. Nếu `prefers-reduced-motion: reduce` thì bỏ qua hoạt cảnh.
  - Mỗi bước `scrollToBottom(behavior: 'auto')` để dòng chữ mới luôn trong tầm mắt.

## 11. File dự kiến

Mới: `pages/chat/chat-sessions.ts|html|scss|spec.ts`, `pages/chat/chat-context-chips.ts|html|scss|spec.ts`, `pages/chat/session-time.ts|spec.ts`, `shared/markdown/stream-reveal.ts|spec.ts`, `core/chat/chat.models.spec.ts`.
Sửa: `pages/chat/chat.ts|html|scss`, `core/chat/chat.models.ts`, `shared/i18n/vi.ts`, `shared/i18n/en.ts`.

## 12. Kiểm chứng

- `npm run lint`, `npm test`, `npm run build` (ngân sách initial + anyComponentStyle không được phình thêm cảnh báo).
- Unit test phủ phần thuần: cắt title (ranh giới từ, rune tiếng Việt, dấu câu treo), `timeAgo` (các ngưỡng + hôm qua), `groupSessions` (ranh giới ngày lịch, bucket rỗng, thứ tự), `splitRevealChunks` (marker nguyên khối, hoàn trả đúng chuỗi gốc), `StreamReveal` (perTick, kết thúc đúng).
- Component test bằng `fixture.detectChanges` + fake timers: chip khoá không phát sự kiện + có tooltip, menu ⋯ có item xóa màu đỏ, ẩn/hiện theo hover là CSS (assert class + tabindex còn trong cây), hoạt cảnh chạy rồi khớp chuỗi cuối.
- **Không tự động test được** (người dùng test tay, checklist ở cuối commit): trạng thái sống qua F5 (title/count đọc lại từ DB), cuộn dọc thật của danh sách dài + không cuộn ngang, drawer overlay trên <768px và scrim/Esc, cảm giác hover reveal nút ⋯, focus ring/tab order mắt thường, đọc màn hình, tương phản AA thực tế trên hai theme, và hiệu ứng stream trông "như đang gõ" hay không.

## 13. Không làm đợt này

- Không SSE/streaming từ backend (API giữ nguyên, đã ghi ở mục 2).
- Không lưu title tự sinh vào localStorage — DB là nguồn.
- Không thêm kéo-thả/rename hàng loạt, không search trong sidebar.
