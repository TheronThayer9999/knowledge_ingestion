/**
 * Cấu hình runtime của FE. Đây là bản mặc định (dùng cho build production và
 * unit test): `apiBaseUrl` tương đối nghĩa là API nằm cùng origin với app khi
 * deploy — khỏi đổi code, khỏi lo CORS.
 *
 * Bản dev (`environment.development.ts`) ghi đè bằng URL tuyệt đối của backend
 * để `ng serve` gọi thẳng :9001, không qua dev-server proxy nữa. Việc thay file
 * do `fileReplacements` trong angular.json lo theo build configuration.
 */
export const environment = {
  apiBaseUrl: '/api/v1',
};
