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
           --content-padding · --content-viewport-height · --content-sticky-offset
           --control-height · --account-menu-max-width · --z-* · --radius-*
           --space-* · --text-style-*
      └─ tiêu đề trang dùng chung : src/styles/_page-head.scss
           .page-title · .page-subtitle (khai báo 1 lần, không copy ở trang)
```

## Quy tắc bắt buộc

1. **Không trang nào tự dựng header/sidebar.** Chỉ `dashboard-layout/` được import
   `@angular/material/sidenav` và `@angular/material/toolbar` (ESLint
   `no-restricted-imports` chặn — xem eslint.config.js). Trang mới = thêm route
   CON vào `children` của `/app` trong `app.routes.ts`.
2. **Không hard-code màu / bo góc / khoảng cách / kích thước khung.** Chỉ dùng
   token trong `src/styles/_tokens.scss` (spacing 4/8/12/16/24/32, radius
   8/16/999, 4 cỡ chữ × 2 độ đậm). Trang cần chiều cao vừa viewport dùng
   `--content-viewport-height`, KHÔNG tự `calc(100dvh - toolbar - padding)`;
   panel sticky dùng `--content-sticky-offset`, KHÔNG tự
   `calc(var(--toolbar-height) + …)`.
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
   `.page-title`/`.page-subtitle` khai báo MỘT lần ở `src/styles/_page-head.scss`,
   trang KHÔNG copy lại; chỉ `.page-head` (bố cục riêng từng trang) nằm trong trang.

## KHÔNG động vào nếu không có yêu cầu rõ ràng

- `src/app/layout/dashboard-layout/**` (shell + shell.config.ts)
- `src/styles/_tokens.scss`, `src/styles/_page-head.scss`, `src/styles/_mixins.scss`,
  `src/styles/styles.scss` (theme/tokens + mixin bố cục dùng chung)
- `app.routes.ts` phần layout cha-con (router structure)
- `index.html` script chống FOUC — đã BỎ danh sách palette trùng lặp, nguồn duy
  nhất là `THEME_PALETTES`; hợp đồng `THEME_STORAGE_KEY` + `THEME_CLASS_PREFIX`
  do `theme.service.spec.ts` khóa lại (thêm palette xem playbook F)
- `index.html` phần `<head>` font — Roboto + Material Icons **tự host** qua npm
  (`@fontsource/roboto`, `material-icons`), `@import` trong `src/styles/_fonts.scss`;
  `angular.json > styles` giữ đúng MỘT entry `styles.scss` (esbuild tự phân giải
  bare specifier từ `node_modules`, không cần ghi đường dẫn cài đặt).
  CẤM thêm link Google Fonts CDN trở lại: môi trường chặn CDN làm `mat-icon` in ra
  chữ (`space_dashboard`) thay vì icon. CI `ci-fe.yml` grep tên host để chặn.

## Playbook mở rộng (làm đúng thứ tự là không rối)

**A. Thêm trang mới** (vd Reports) — 4 bước, thiếu 1 bước là test/lint fail:
1. Tạo `src/app/pages/reports/` (`.ts` `.html` `.scss` `.spec.ts`) — component chỉ
   trả về `.page` + `.page-head`, KHÔNG dựng header/sidebar. `.page-head` dùng
   mixin `page-head-split(<bp>)` (row + action phải) hoặc `page-head-stack` (cột)
   từ `src/styles/_mixins.scss` — không viết lại rule bố cục trong file trang.
2. `app.routes.ts`: thêm route CON vào `children` của `/app` (loadComponent lazy).
3. `shell.config.ts`: thêm 1 dòng `{ path: '/app/reports', icon: '...', label: 'NAV_REPORTS' }`
   vào `NAV_GROUPS` → menu + title topbar tự ra, không sửa template shell.
4. i18n: thêm `NAV_REPORTS` vào `src/app/shared/i18n/vi.ts` VÀ `en.ts` (thiếu là build fail).
→ `shell-routes.spec.ts` kiểm 2 chiều route con ↔ mục nav, nên bước 2/3 lệch nhau là đỏ.

**B. Thêm nút góc phải header**: 1 dòng vào `HEADER_ACTIONS` (component tự lấy
state qua service — không nhận `@Input` từ shell).

**C. Trang cần action/panel riêng** (nút upload, filter, toggle panel): đặt trong
`.page-head` hoặc body của CHÍNH trang đó (như `.panel-toggle` trong `chat.scss`) —
không sửa `dashboard-layout.html`. Muốn có slot thật trong shell → xin duyệt (quy tắc 4).

**D. Cần giá trị size/màu mới**: thêm token vào `_tokens.scss` (primitive rồi mới
đến alias), không viết số trong `.scss` của trang. Trang cần cao vừa viewport thì
dùng `--content-viewport-height`.

**E. Cần component dùng chung**: chỉ dời sang `src/app/shared/` khi CÓ TỪ 2 nơi dùng;
1 trang dùng thì để trong thư mục trang đó (tránh shared phình).

**F. Thêm palette theme mới**: 1 phần tử vào `THEME_PALETTES` (`core/theme/theme.service.ts`)
+ 1 block `.theme-<id>` trong `styles.scss`. `index.html` KHÔNG phải sửa — script FOUC chỉ
áp định dạng `theme-<palette>` và palette lạ tự rơi về mặc định của `:root`.

**G. Cần thêm chữ/cỡ font mới (weight, subset)**: thêm dòng `@import '@fontsource/roboto/<subset>-<weight>.css';`
vào `src/styles/_fonts.scss` — KHÔNG tải font bằng tay, KHÔNG dùng link CDN, KHÔNG ghi đường dẫn
`node_modules/...` vào `angular.json`.

**H. Mỗi lớp tự bắt lỗi riêng** — sửa gì thì chạy cái đó:
lệch menu/route → `npm test` · dựng chrome ngoài shell → `npm run lint` ·
trang phình to → budget `anyComponentStyle` 10kB của build · lệch theme → build fail vì thiếu key i18n.

## Bảo vệ

- ESLint `no-restricted-imports`: chặn sidenav/toolbar/shell ngoài `layout/`.
- `src/app/layout/dashboard-layout/shell-routes.spec.ts`: mọi route đăng nhập là
  con của shell, mỗi route con có mục nav tương ứng, trang guest nằm ngoài shell.
- Chạy trước khi commit: `npm run build` · `npm run lint` · `npm test -- --watch=false`.
