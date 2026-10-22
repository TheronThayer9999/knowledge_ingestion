import { TranslationKey } from './lang.model';

/**
 * Bảng dịch TIẾNG ANH — cũng là map phẳng key → value.
 * Kiểu Record<TranslationKey, string> ép đủ bộ key của VI:
 * thiếu key, thừa key, hoặc value không phải string đều lỗi build.
 */
export const EN: Record<TranslationKey, string> = {
  'app.name': 'Knowledge Ingestion',
  'lang.label': 'Language',
  'lang.vi': 'Tiếng Việt',
  'lang.en': 'English',
  'theme.label': 'Theme',
  'login.title': 'Sign in',
  'login.username': 'Username',
  'login.password': 'Password',
  'login.username_error': 'Username is required (at least 3 characters)',
  'login.password_error': 'Enter a password (at least 4 characters)',
  'login.show_password': 'Show password',
  'login.hide_password': 'Hide password',
  'login.submit': 'Sign in',
  'login.demo_hint': 'Demo: {{username}} / {{password}}',
  'login.errors.invalid_credentials': 'Wrong username or password',
  'login.errors.generic': 'Sign in failed, please try again',
  'top_bar.logout': 'Log out',
  'dashboard.stats.categories': 'Categories',
  'dashboard.stats.articles': 'Articles',
  'dashboard.stats.chunks': 'Indexed chunks',
  'dashboard.recent.title': 'Recent articles',
  'dashboard.recent.subtitle': 'Hi {{name}} — your session is active',
  'dashboard.columns.title': 'Title',
  'dashboard.columns.category': 'Category',
  'dashboard.columns.status': 'Status',
  'dashboard.columns.indexed_at': 'Indexed at',
};
