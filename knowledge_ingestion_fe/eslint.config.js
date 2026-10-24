// @ts-check
const eslint = require('@eslint/js');
const tseslint = require('typescript-eslint');
const angular = require('angular-eslint');
const prettier = require('eslint-config-prettier/flat');

/**
 * ESLint 9 flat config cho Angular: rule TypeScript + Angular (ts + template),
 * `angular.processInlineTemplates` để lint cả template viết inline trong .ts.
 * `eslint-config-prettier` đứng CUỐI để tắt các rule format trùng Prettier.
 */
module.exports = tseslint.config(
  {
    ignores: ['dist/**', '.angular/**', 'node_modules/**'],
  },
  {
    files: ['**/*.ts'],
    extends: [
      eslint.configs.recommended,
      ...tseslint.configs.recommended,
      ...tseslint.configs.stylistic,
      ...angular.configs.tsRecommended,
    ],
    processor: angular.processInlineTemplates,
    rules: {
      '@angular-eslint/directive-selector': [
        'error',
        { type: 'attribute', prefix: 'app', style: 'camelCase' },
      ],
      '@angular-eslint/component-selector': [
        'error',
        { type: 'element', prefix: 'app', style: 'kebab-case' },
      ],
    },
  },
  {
    // Khóa APP SHELL (xem ARCHITECTURE.md): chỉ src/app/layout/** được dựng
    // sidenav/toolbar hay import DashboardLayout/shell.config trực tiếp.
    // `ignores` đứng cùng object với `files` là cách flat config loại trừ phạm vi.
    files: ['src/**/*.ts'],
    ignores: ['src/app/layout/**'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          paths: [
            {
              name: '@angular/material/sidenav',
              message: 'APP SHELL duy nhất là DashboardLayout (src/app/layout/). Trang mới = thêm route con vào app.routes.ts — xem ARCHITECTURE.md.',
            },
            {
              name: '@angular/material/toolbar',
              message: 'APP SHELL duy nhất là DashboardLayout (src/app/layout/). Trang mới = thêm route con vào app.routes.ts — xem ARCHITECTURE.md.',
            },
          ],
          patterns: [
            {
              group: ['**/dashboard-layout', '**/shell.config'],
              message: 'Không import nội bộ shell ngoài layout/ — menu/header qua route con + NAV_GROUPS/HEADER_ACTIONS (shell.config.ts).',
            },
          ],
        },
      ],
    },
  },
  {
    files: ['**/*.html'],
    extends: [...angular.configs.templateRecommended, ...angular.configs.templateAccessibility],
    rules: {},
  },
  prettier,
);
