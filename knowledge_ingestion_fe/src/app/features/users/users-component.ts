import { DatePipe } from '@angular/common';
import { Component, inject, signal } from '@angular/core';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatTableModule } from '@angular/material/table';
import { User } from '../../core/auth/models/auth.model';
import { UsersService } from '../../core/api/users.service';
import { ApiRequestError } from '../../core/api/api.helpers';
import { MessageService } from '../../shared/feedback/message.service';
import { LangService } from '../../shared/i18n/lang.service';
import { TranslationKey } from '../../shared/i18n/lang.model';

type UserColumn = 'username' | 'email' | 'phone' | 'role' | 'active' | 'lastLogin';

/** Tên cột bảng → key i18n (SCREAMING_SNAKE trong vi.json/en.json). */
const COLUMN_KEYS: Record<UserColumn, TranslationKey> = {
  username: 'USERS_COL_USERNAME',
  email: 'USERS_COL_EMAIL',
  phone: 'USERS_COL_PHONE',
  role: 'USERS_COL_ROLE',
  active: 'USERS_COL_ACTIVE',
  lastLogin: 'COMMON_LAST_LOGIN',
};

/** Danh sách người dùng (GET /users) — chỉ đọc, chưa có thao tác sửa user khác. */
@Component({
  selector: 'app-users',
  imports: [DatePipe, MatCardModule, MatChipsModule, MatTableModule],
  templateUrl: './users-component.html',
  styleUrl: './users-component.scss',
})
export class UsersComponent {
  private readonly usersApi = inject(UsersService);
  private readonly messages = inject(MessageService);
  protected readonly lang = inject(LangService);

  protected readonly columns: UserColumn[] = ['username', 'email', 'phone', 'role', 'active', 'lastLogin'];
  protected readonly users = signal<User[]>([]);
  protected readonly total = signal(0);
  protected readonly loading = signal(true);

  constructor() {
    this.usersApi.list().subscribe({
      next: (res) => {
        this.users.set(res.users);
        this.total.set(res.total);
        this.loading.set(false);
      },
      error: (err: unknown) => {
        this.loading.set(false);
        if (err instanceof ApiRequestError) {
          this.messages.raw('error', err.message);
        } else {
          this.messages.error('LOGIN_ERRORS_GENERIC');
        }
      },
    });
  }

  protected columnKey(col: UserColumn): TranslationKey {
    return COLUMN_KEYS[col];
  }

  protected roleKey(user: User): TranslationKey {
    return user.role >= 1 ? 'USERS_ROLE_ADMIN' : 'USERS_ROLE_USER';
  }

  protected activeKey(user: User): TranslationKey {
    return user.active ? 'USERS_ACTIVE_YES' : 'USERS_ACTIVE_NO';
  }
}
