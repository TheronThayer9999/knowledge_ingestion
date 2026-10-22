import { Component, inject, signal } from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIcon } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatProgressSpinnerModule } from '@angular/material/progress-spinner';
import { ActivatedRoute, Router, RouterLink } from '@angular/router';
import { AuthService } from '../../core/auth/services/auth.service';
import { AuthApiError, AuthErrorCode } from '../../core/auth/models/auth-error.model';
import { MessageService } from '../../shared/feedback/message.service';
import { LangService } from '../../shared/i18n/lang.service';
import { TranslationKey } from '../../shared/i18n/lang.model';

/** Mã lỗi auth → key i18n (SCREAMING_SNAKE trong vi.json/en.json). */
const AUTH_ERROR_KEYS: Record<AuthErrorCode, TranslationKey> = {
  INVALID_CREDENTIALS: 'LOGIN_ERRORS_INVALID_CREDENTIALS',
  ACCOUNT_LOCKED: 'LOGIN_ERRORS_ACCOUNT_LOCKED',
  USERNAME_TAKEN: 'LOGIN_ERRORS_USERNAME_TAKEN',
  GENERIC: 'LOGIN_ERRORS_GENERIC',
};

/** Trang login: form username/password, gọi AuthService (mock API), guard chặn khách. */
@Component({
  selector: 'app-login',
  imports: [
    ReactiveFormsModule,
    RouterLink,
    MatCardModule,
    MatFormFieldModule,
    MatInputModule,
    MatButtonModule,
    MatIcon,
    MatProgressSpinnerModule,
  ],
  templateUrl: './login-component.html',
  styleUrl: './login-component.scss',
})
export class LoginComponent {
  private readonly fb = inject(FormBuilder);
  private readonly auth = inject(AuthService);
  private readonly router = inject(Router);
  private readonly route = inject(ActivatedRoute);
  private readonly messages = inject(MessageService);
  protected readonly lang = inject(LangService);

  protected readonly submitting = signal(false);
  protected readonly googleLoading = signal(false);
  protected readonly hidePassword = signal(true);

  protected readonly form = this.fb.nonNullable.group({
    username: ['', [Validators.required, Validators.minLength(3)]],
    password: ['', [Validators.required, Validators.minLength(4)]],
  });

  protected submit(): void {
    if (this.form.invalid) {
      this.form.markAllAsTouched();
      return;
    }
    this.submitting.set(true);

    this.auth.login(this.form.getRawValue()).subscribe({
      next: () => {
        this.submitting.set(false);
        // Mọi message bắn ra từ trung tâm — không còn block inline trong form.
        this.messages.success('LOGIN_SUCCESS');
        const returnUrl = this.route.snapshot.queryParamMap.get('returnUrl') ?? '/dashboard';
        void this.router.navigateByUrl(returnUrl);
      },
      error: (err: unknown) => {
        this.submitting.set(false);
        const code: AuthErrorCode = err instanceof AuthApiError ? err.code : 'GENERIC';
        this.messages.error(AUTH_ERROR_KEYS[code]);
      },
    });
  }

  /** Đăng nhập Google (mock OAuth) — cùng luồng phiên với form login. */
  protected loginWithGoogle(): void {
    if (this.googleLoading()) {
      return;
    }
    this.googleLoading.set(true);
    this.auth.loginWithGoogle().subscribe({
      next: () => {
        this.googleLoading.set(false);
        this.messages.success('LOGIN_SUCCESS');
        this.goToReturnUrl();
      },
      error: () => {
        this.googleLoading.set(false);
        this.messages.error(AUTH_ERROR_KEYS['GENERIC']);
      },
    });
  }

  private goToReturnUrl(): void {
    const returnUrl = this.route.snapshot.queryParamMap.get('returnUrl') ?? '/dashboard';
    void this.router.navigateByUrl(returnUrl);
  }

  protected togglePasswordVisibility(): void {
    this.hidePassword.update((v) => !v);
  }
}
