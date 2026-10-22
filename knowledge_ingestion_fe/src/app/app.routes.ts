import { Routes } from '@angular/router';
import { adminGuard, authGuard, guestGuard } from './core/guards/auth.guard';

export const routes: Routes = [
  {
    // Shell đăng nhập: TopBar + nav dùng chung, mọi trang con guard auth.
    path: '',
    canActivate: [authGuard],
    loadComponent: () => import('./features/shell/shell-component').then((m) => m.ShellComponent),
    children: [
      { path: '', pathMatch: 'full', redirectTo: 'dashboard' },
      {
        path: 'dashboard',
        loadComponent: () => import('./features/dashboard/dashboard-component').then((m) => m.DashboardComponent),
      },
      {
        path: 'categories',
        loadComponent: () => import('./features/categories/categories-component').then((m) => m.CategoriesComponent),
      },
      {
        path: 'articles',
        loadComponent: () => import('./features/articles/articles-component').then((m) => m.ArticlesComponent),
      },
      {
        path: 'articles/:id',
        loadComponent: () => import('./features/articles/article-detail-component').then((m) => m.ArticleDetailComponent),
      },
      {
        path: 'search',
        loadComponent: () => import('./features/search/search-component').then((m) => m.SearchComponent),
      },
      {
        path: 'chat',
        loadComponent: () => import('./features/chat/chat-component').then((m) => m.ChatComponent),
      },
      {
        path: 'users',
        loadComponent: () => import('./features/users/users-component').then((m) => m.UsersComponent),
      },
      {
        path: 'profile',
        loadComponent: () => import('./features/profile/profile-component').then((m) => m.ProfileComponent),
      },
      {
        path: 'admin',
        canActivate: [adminGuard],
        loadComponent: () => import('./features/admin/admin-component').then((m) => m.AdminComponent),
      },
      { path: '**', redirectTo: 'dashboard' },
    ],
  },
  {
    path: 'login',
    canActivate: [guestGuard],
    loadComponent: () => import('./features/login/login-component').then((m) => m.LoginComponent),
  },
  {
    path: 'register',
    canActivate: [guestGuard],
    loadComponent: () => import('./features/register/register-component').then((m) => m.RegisterComponent),
  },
  { path: '**', redirectTo: 'dashboard' },
];
