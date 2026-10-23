import { DatePipe } from '@angular/common';
import { Component, inject, signal } from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { Router } from '@angular/router';
import { filter } from 'rxjs';
import { User } from '../../core/auth/models/auth.model';
import { AuthService } from '../../core/auth/services/auth.service';
import { AccountService } from '../../core/api/account.service';
import { ApiRequestError } from '../../core/api/api.helpers';
import { MessageService } from '../../shared/feedback/message.service';
import { LangService } from '../../shared/i18n/lang.service';
import { TranslationKey } from '../../shared/i18n/lang.model';

/**
 * Trang tài khoản của chính mình: thông tin (GET /auth/profile — dữ liệu mới nhất),
 * đổi mật khẩu (POST /auth/change-password), thu hồi mọi phiên (POST /auth/logout-all)
 * rồi đăng xuất cục bộ + về login.
 */
@Component({
  selector: 'app-profile',
  imports: [
    DatePipe,
    ReactiveFormsModule,
    MatButtonModule,
    MatCardModule,
    MatChipsModule,
    MatFormFieldModule,
    MatIconModule,
    MatInputModule,
  ],
  templateUrl: './profile-component.html',
  styleUrl: './profile-component.scss',
})
export class ProfileComponent {
  private readonly accountApi = inject(AccountService);
  private readonly auth = inject(AuthService);
  private readonly router = inject(Router);
  private readonly messages = inject(MessageService);
  private readonly fb = inject(FormBuilder);
  protected readonly lang = inject(LangService);

  /** Bản từ BE (fresh) — fallback về user trong store auth khi API lỗi. */
  protected readonly profile = signal<User | null>(this.auth.user());
  protected readonly submitting = signal(false);

  protected readonly passwordForm = this.fb.nonNullable.group({
    oldPassword: ['', [Validators.required]],
    newPassword: ['', [Validators.required, Validators.minLength(8)]],
    confirmPassword: ['', [Validators.required]],
  });

  constructor() {
    this.accountApi.profile().subscribe({
      next: (u) => this.profile.set(u),
      error: (err: unknown) => this.showError(err),
    });
  }

  protected get passwordsMismatch(): boolean {
    const c = this.passwordForm.controls;
    return c.confirmPassword.value !== '' && c.newPassword.value !== c.confirmPassword.value;
  }

  protected changePassword(): void {
    if (this.passwordForm.invalid || this.submitting()) {
      return;
    }
    const v = this.passwordForm.getRawValue();
    if (v.newPassword !== v.confirmPassword) {
      this.messages.error('PROFILE_PASSWORD_MISMATCH');
      return;
    }
    this.submitting.set(true);
    this.accountApi.changePassword({ old_password: v.oldPassword, new_password: v.newPassword }).subscribe({
      next: () => {
        this.submitting.set(false);
        this.messages.success('PROFILE_PASSWORD_CHANGED');
        this.passwordForm.reset();
      },
      error: (err: unknown) => {
        this.submitting.set(false);
        this.showError(err);
      },
    });
  }

  /** Thu hồi mọi refresh token trên server, kết thúc session luôn phía FE. */
  protected logoutAll(): void {
    this.messages
      .confirm('COMMON_CONFIRM', 'PROFILE_LOGOUT_ALL_CONFIRM')
      .pipe(filter(Boolean))
      .subscribe(() => {
        this.accountApi.logoutAll().subscribe({
          next: (res) => {
            this.messages.raw('success', this.lang.t('PROFILE_LOGOUT_ALL_DONE', { count: res.revoked }));
            this.auth.logout();
            void this.router.navigate(['/login']);
          },
          error: (err: unknown) => this.showError(err),
        });
      });
  }

  protected roleKey(user: User): TranslationKey {
    return user.role >= 1 ? 'USERS_ROLE_ADMIN' : 'USERS_ROLE_USER';
  }

  private showError(err: unknown): void {
    if (err instanceof ApiRequestError) {
      this.messages.raw('error', err.message);
    } else {
      this.messages.error('LOGIN_ERRORS_GENERIC');
    }
  }
}
