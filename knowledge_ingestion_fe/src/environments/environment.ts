/**
 * Cấu hình runtime của FE. `apiBaseUrl` dùng đường dẫn tương đối để:
 * - dev: Angular dev-server proxy `/api` → backend :9001 (không phụ thuộc CORS)
 * - prod: cùng origin với API khi deploy
 */
export const environment = {
  apiBaseUrl: '/api/v1',
};
