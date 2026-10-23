import { DatePipe } from '@angular/common';
import { Component, computed, inject, signal } from '@angular/core';
import {
  FormBuilder,
  ReactiveFormsModule,
  Validators,
  type AbstractControl,
  type ValidationErrors,
} from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatProgressSpinnerModule } from '@angular/material/progress-spinner';
import { Router } from '@angular/router';
import { finalize } from 'rxjs';
import { AuthService } from '../../core/auth/auth.service';
import { LangService, type TranslationKey } from '../../core/i18n/lang.service';
import { MessageService } from '../../shared/feedback/message.service';

type PasswordField = 'old' | 'new' | 'confirm';

/** Mật khẩu mới và ô nhập lại phải trùng — validator mức group */
function passwordsMatch(group: AbstractControl): ValidationErrors | null {
  const password = group.get('newPassword')?.value as string | undefined;
  const confirm = group.get('confirmPassword')?.value as string | undefined;
  return password === confirm ? null : { mismatch: true };
}

/**
 * Hồ sơ cá nhân: thông tin tài khoản (chỉ đọc — backend chưa có API cập nhật
 * hồ sơ), đổi mật khẩu và đăng xuất mọi thiết bị. Đổi mật khẩu thành công
 * thì mọi phiên bị thu hồi nên đưa người dùng về trang đăng nhập.
 */
@Component({
  selector: 'app-profile',
  imports: [
    DatePipe,
    ReactiveFormsModule,
    MatCardModule,
    MatFormFieldModule,
    MatInputModule,
    MatButtonModule,
    MatIconModule,
    MatProgressSpinnerModule,
  ],
  templateUrl: './profile.html',
  styleUrl: './profile.scss',
})
export class Profile {
  protected readonly auth = inject(AuthService);
  protected readonly lang = inject(LangService);

  private readonly messages = inject(MessageService);
  private readonly router = inject(Router);

  protected readonly saving = signal(false);
  protected readonly signingOut = signal(false);
  private readonly visible = signal<ReadonlySet<PasswordField>>(new Set());

  protected readonly form;
  protected readonly user = this.auth.user;

  protected readonly initials = computed(() => {
    const name = this.user()?.username ?? '';
    return name.slice(0, 2).toUpperCase();
  });

  protected readonly roleKey = computed<TranslationKey>(() =>
    this.auth.isAdmin() ? 'PROFILE_ROLE_ADMIN' : 'PROFILE_ROLE_USER',
  );

  protected readonly statusKey = computed<TranslationKey>(() =>
    this.user()?.active === false ? 'PROFILE_STATUS_INACTIVE' : 'PROFILE_STATUS_ACTIVE',
  );

  constructor() {
    const fb = inject(FormBuilder);
    this.form = fb.nonNullable.group(
      {
        oldPassword: ['', [Validators.required]],
        newPassword: ['', [Validators.required, Validators.minLength(8)]],
        confirmPassword: ['', [Validators.required]],
      },
      { validators: passwordsMatch },
    );

    // Làm mới thông tin từ server (last_login, role...) — lỗi thì giữ dữ liệu phiên
    this.auth.loadProfile().subscribe({ error: () => undefined });
  }

  protected isVisible(field: PasswordField): boolean {
    return this.visible().has(field);
  }

  protected toggleVisibility(field: PasswordField): void {
    this.visible.update((set) => {
      const next = new Set(set);
      if (next.has(field)) {
        next.delete(field);
      } else {
        next.add(field);
      }
      return next;
    });
  }

  protected submit(): void {
    if (this.saving()) {
      return;
    }
    if (this.form.invalid) {
      this.form.markAllAsTouched();
      return;
    }
    const { oldPassword, newPassword } = this.form.getRawValue();
    this.saving.set(true);
    this.auth
      .changePassword(oldPassword, newPassword)
      .pipe(finalize(() => this.saving.set(false)))
      .subscribe({
        next: () => {
          this.messages.success('PROFILE_PASSWORD_CHANGED');
          void this.router.navigate(['/login']);
        },
        error: (error: unknown) => this.messages.errorFromApi(error),
      });
  }

  protected async logoutAll(): Promise<void> {
    if (this.signingOut()) {
      return;
    }
    const confirmed = await this.messages.confirm(
      'PROFILE_LOGOUT_ALL_CONFIRM_TITLE',
      'PROFILE_LOGOUT_ALL_CONFIRM_MESSAGE',
    );
    if (!confirmed) {
      return;
    }
    this.signingOut.set(true);
    this.auth
      .logoutAll()
      .pipe(finalize(() => this.signingOut.set(false)))
      .subscribe({
        next: () => {
          this.messages.success('PROFILE_LOGGED_OUT_ALL');
          void this.router.navigate(['/login']);
        },
        error: (error: unknown) => this.messages.errorFromApi(error),
      });
  }
}
