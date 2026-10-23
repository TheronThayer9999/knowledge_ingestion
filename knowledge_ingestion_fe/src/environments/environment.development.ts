/**
 * Bản dev — `ng serve` (build configuration "development") thay vào đây qua
 * `fileReplacements` trong angular.json. FE gọi thẳng backend :9001, không còn
 * dev-server proxy. Đổi lại trình duyệt coi đây là cross-origin nên origin phải
 * có mặt trong `cors.allowed_origins` của config backend (mặc định
 * http://localhost:4200 và http://127.0.0.1:4200).
 */
export const environment = {
  apiBaseUrl: 'http://127.0.0.1:9001/api/v1',
};
