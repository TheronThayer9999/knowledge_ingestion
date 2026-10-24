// Khóa cấu trúc APP SHELL (xem ARCHITECTURE.md): mọi route đăng nhập phải là
// con của shell /app, mỗi route con phải có mục nav tương ứng trong
// shell.config.ts, trang guest nằm ngoài shell. Sửa route mà quên sửa nav là
// test này fail — không cần chạy TestBed vì chỉ đọc metadata Routes.
import { routes } from '../../app.routes';
import { NAV_GROUPS } from './shell.config';

/** Route cha của shell — path 'app', load DashboardLayout */
function findShellRoute() {
  const shell = routes.find((r) => r.path === 'app');
  if (!shell) throw new Error('Route /app (APP SHELL) không tồn tại trong app.routes.ts');
  return shell;
}

describe('APP SHELL route structure', () => {
  it('shell /app là route cha có children load DashboardLayout', () => {
    const shell = findShellRoute();
    expect(shell.children?.length).toBeGreaterThan(0);
    // loadComponent là function động — chỉ cần tồn tại, không gọi trong test
    expect(typeof shell.loadComponent).toBe('function');
    expect(shell.canActivate?.length).toBeGreaterThan(0); // authGuard
  });

  it('mỗi route con của shell (trừ redirect) có đúng một mục nav trong NAV_GROUPS', () => {
    const childPaths = (findShellRoute().children ?? [])
      .filter((child) => !(child.path === '' && 'redirectTo' in child && child.pathMatch === 'full'))
      .map((child) => `/app/${child.path}`);
    const navPaths = NAV_GROUPS.flatMap((group) => group.items).map((item) => item.path);

    expect(childPaths).not.toHaveLength(0);
    // 2 chiều: route con nào cũng lên menu, mục menu nào cũng trỏ route thật
    for (const path of childPaths) {
      expect(navPaths).toContain(path);
    }
    for (const path of navPaths) {
      expect(childPaths).toContain(path);
    }
  });

  it('không route con nào của shell tự khai báo sidenav/toolbar riêng', () => {
    // route con phải là lá (không có children lồng) — mọi trang cùng nằm
    // trong MỘT shell, không có "shell trong shell"
    for (const child of findShellRoute().children ?? []) {
      expect(child.children).toBeUndefined();
    }
  });

  it('trang guest login/register nằm ngoài shell (top-level, không lồng /app)', () => {
    for (const guestPath of ['login', 'register']) {
      const guest = routes.find((r) => r.path === guestPath);
      expect(guest).toBeDefined();
      expect(guest?.children).toBeUndefined();
    }
  });
});
