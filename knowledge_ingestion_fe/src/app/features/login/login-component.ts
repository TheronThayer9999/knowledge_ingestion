import { Component, inject, signal } from '@angular/core';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIcon } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatProgressSpinnerModule } from '@angular/material/progress-spinner';
import { ActivatedRoute, Router } from '@angular/router';
import { AuthService } from '../../core/auth/services/auth.service';
import { AuthApiError, AuthErrorCode } from '../../core/auth/models/auth-error.model';
import { LangService } from '../../shared/i18n/lang.service';
import { TranslationKey } from '../../shared/i18n/lang.model';

/** Mã lỗi auth → key i18n (key chữ thường trong map vi.ts/en.ts). */
const AUTH_ERROR_KEYS: Record<AuthErrorCode, TranslationKey> = {
  INVALID_CREDENTIALS: 'login.errors.invalid_credentials',
  GENERIC: 'login.errors.generic',
};

/** Trang login: form username/password, gọi AuthService (mock API), guard chặn khách. */
@Component({
  selector: 'app-login',
  imports: [
    ReactiveFormsModule,
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
  protected readonly lang = inject(LangService);

  /** Key i18n của lỗi gần nhất — hiển thị bằng lang.t(key). */
  protected readonly errorKey = signal<TranslationKey | null>(null);
  protected readonly submitting = signal(false);
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
    this.errorKey.set(null);

    this.auth.login(this.form.getRawValue()).subscribe({
      next: () => {
        this.submitting.set(false);
        const returnUrl = this.route.snapshot.queryParamMap.get('returnUrl') ?? '/dashboard';
        void this.router.navigateByUrl(returnUrl);
      },
      error: (err: unknown) => {
        this.submitting.set(false);
        const code: AuthErrorCode = err instanceof AuthApiError ? err.code : 'GENERIC';
        this.errorKey.set(AUTH_ERROR_KEYS[code]);
      },
    });
  }

  protected togglePasswordVisibility(): void {
    this.hidePassword.update((v) => !v);
  }
}
