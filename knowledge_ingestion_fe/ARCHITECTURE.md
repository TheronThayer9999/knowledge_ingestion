# ARCHITECTURE.md — APP SHELL (frontend Angular)

> Áp dụng cho `knowledge_ingestion_fe/`. Mục tiêu: khung ngoài (Header, Sidebar,
> vùng Content) nằm ở MỘT nơi duy nhất và không bị các trang sửa lẻ tẻ.

## Sơ đồ shell

```
src/main.ts
└─ App (app.html = <router-outlet/> thuần túy — KHÔNG chrome tại đây)
   └─ app.routes.ts
      ├─ /app → DashboardLayout  ← APP SHELL DUY NHẤT (vùng đăng nhập)
      │    src/app/layout/dashboard-layout/
      │    ├─ dashboard-layout.ts/html/scss   khung: sidenav + toolbar + content
      │    └─ shell.config.ts                 NAV_GROUPS · HEADER_ACTIONS · DEFAULT_PAGE_TITLE
      │    ├─ HEADER  : title theo route + HEADER_ACTIONS (lang, theme) + avatar/menu tài khoản
      │    ├─ SIDEBAR : brand + @for NAV_GROUPS + nút thu gọn rail/drawer
      │    └─ CONTENT : main.shell-content > router-outlet (Overview/Documents/Chat/Settings/Profile)
      ├─ /login, /register → trang guest, CÓ CHỦ ĐÍCH nằm ngoài shell
      │    (tự đặt Language/ThemeSwitcher góc phải — không được dựng header/sidebar)
      └─ tokens khung : src/styles/_tokens.scss
           --toolbar-height · --sidebar-width · --sidebar-width-rail
           --content-padding · --content-viewport-height · --control-height
           --account-menu-max-width · --z-* · --radius-* · --space-* · --text-style-*
```

## Quy tắc bắt buộc

1. **Không trang nào tự dựng header/sidebar.** Chỉ `dashboard-layout/` được import
   `@angular/material/sidenav` và `@angular/material/toolbar` (ESLint
   `no-restricted-imports` chặn — xem eslint.config.js). Trang mới = thêm route
   CON vào `children` của `/app` trong `app.routes.ts`.
2. **Không hard-code màu / bo góc / khoảng cách / kích thước khung.** Chỉ dùng
   token trong `src/styles/_tokens.scss` (spacing 4/8/12/16/24/32, radius
   8/16/999, 4 cỡ chữ × 2 độ đậm). Trang cần chiều cao vừa viewport dùng
   `--content-viewport-height`, KHÔNG tự `calc(100dvh - toolbar - padding)`.
3. **Thêm mục menu chỉ qua `shell.config.ts`**: 1 dòng vào `NAV_GROUPS`
   (kèm route con tương ứng trong `app.routes.ts` + key i18n `NAV_*` trong
   `src/app/shared/i18n/vi.ts` và `en.ts`). Thêm nút trên header: 1 dòng vào
   `HEADER_ACTIONS`. Không viết cứng markup menu/action trong template.
4. **Mọi thay đổi trong `src/app/layout/` phải được chủ dự án duyệt trước.**
5. **Làm việc trong một trang chỉ sửa file của trang đó**
   (`src/app/pages/<ten-trang>/**`). Trang cần nút/panel riêng trên header thì
   dùng slot trong trang (page-head, panel toggle như `chat.scss`), không sửa shell.
6. Title trang: topbar lấy từ `NAV_GROUPS` theo URL; trong nội dung trang có
   `.page-head` (title + subtitle + action) — cả hai lớp là chủ đích, giữ nguyên.

## KHÔNG động vào nếu không có yêu cầu rõ ràng

- `src/app/layout/dashboard-layout/**` (shell + shell.config.ts)
- `src/styles/_tokens.scss`, `src/styles/styles.scss` (theme/tokens)
- `app.routes.ts` phần layout cha-con (router structure)
- `index.html` script chống FOUC (song song với `core/theme/theme.service.ts` —
  thêm palette phải sửa CẢ HAI nơi theo chủ đích)

## Bảo vệ

- ESLint `no-restricted-imports`: chặn sidenav/toolbar/shell ngoài `layout/`.
- `src/app/layout/dashboard-layout/shell-routes.spec.ts`: mọi route đăng nhập là
  con của shell, mỗi route con có mục nav tương ứng, trang guest nằm ngoài shell.
- Chạy trước khi commit: `npm run build` · `npm run lint` · `npm test -- --watch=false`.
