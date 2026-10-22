/**
 * Bảng dịch TIẾNG VIỆT — map PHẲNG key → value, key chữ thường, phân cấp bằng dấu chấm.
 * VI là NGUỒN CHUẨN về key (keyof typeof VI = TranslationKey):
 * en.ts được ép kiểu Record<TranslationKey, string> nên thiếu/thừa key là lỗi build.
 */
export const VI = {
  'app.name': 'Knowledge Ingestion',
  'lang.label': 'Ngôn ngữ',
  'lang.vi': 'Tiếng Việt',
  'lang.en': 'English',
  'theme.label': 'Chủ đề',
  'login.title': 'Đăng nhập',
  'login.username': 'Tài khoản',
  'login.password': 'Mật khẩu',
  'login.username_error': 'Tài khoản là bắt buộc (tối thiểu 3 ký tự)',
  'login.password_error': 'Nhập mật khẩu (tối thiểu 4 ký tự)',
  'login.show_password': 'Hiện mật khẩu',
  'login.hide_password': 'Ẩn mật khẩu',
  'login.submit': 'Đăng nhập',
  'login.demo_hint': 'Demo: {{username}} / {{password}}',
  'login.errors.invalid_credentials': 'Sai tài khoản hoặc mật khẩu',
  'login.errors.generic': 'Đăng nhập thất bại, vui lòng thử lại',
  'top_bar.logout': 'Đăng xuất',
  'dashboard.stats.categories': 'Danh mục',
  'dashboard.stats.articles': 'Tài liệu',
  'dashboard.stats.chunks': 'Chunk đã index',
  'dashboard.recent.title': 'Tài liệu gần đây',
  'dashboard.recent.subtitle': 'Chào {{name}} — phiên đăng nhập đang hoạt động',
  'dashboard.columns.title': 'Tên tài liệu',
  'dashboard.columns.category': 'Danh mục',
  'dashboard.columns.status': 'Trạng thái',
  'dashboard.columns.indexed_at': 'Ngày index',
};
