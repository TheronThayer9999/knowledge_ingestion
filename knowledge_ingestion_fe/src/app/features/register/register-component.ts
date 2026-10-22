import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';
import { AbstractControl, FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { MatAnchor, MatButton } from '@angular/material/button';
import { MatCard, MatCardContent, MatCardHeader, MatCardTitle } from '@angular/material/card';
import { MatError, MatFormField, MatLabel, MatSuffix } from '@angular/material/form-field';
import { MatIcon } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatProgressSpinnerModule } from '@angular/material/progress-spinner';
import { Router, RouterLink } from '@angular/router';
import { AuthApiError, AuthErrorCode } from '../../core/auth/models/auth-error.model';
import { AuthService } from '../../core/auth/services/auth.service';
import { MessageService } from '../../shared/feedback/message.service';
import { LangService } from '../../shared/i18n/lang.service';
import { TranslationKey } from '../../shared/i18n/lang.model';

/** Mã lỗi auth từ API → key i18n — đăng ký trùng tên thì báo rõ. */
const REGISTER_ERROR_KEYS: Record<AuthErrorCode, TranslationKey> = {
  INVALID_CREDENTIALS: 'LOGIN_ERRORS_INVALID_CREDENTIALS',
  USERNAME_TAKEN: 'LOGIN_ERRORS_USERNAME_TAKEN',
  GENERIC: 'LOGIN_ERRORS_GENERIC',
};

/** Validator so khớp mật khẩu nhập lại — gắn ở control confirmPassword. */
const mustMatchPassword = (control: AbstractControl): { [key: string]: boolean } | null => {
  const password = control.parent?.get('password');
  return !!password && control.value !== password.value ? { mustMatch: true } : null;
};

/**
 * Trang tạo tài khoản — mock register, thành công là có phiên luôn
 * (withSession trong AuthService đã persist + broadcast sang tab khác).
 */
@Component({
  selector: 'app-register',
  standalone: true,
  imports: [
    ReactiveFormsModule,
    RouterLink,
    MatCard,
    MatCardHeader,
    MatCardContent,
    MatCardTitle,
    MatFormField,
    MatLabel,
    MatError,
    MatSuffix,
    MatIcon,
    MatInputModule,
    MatButton,
    MatAnchor,
    MatProgressSpinnerModule,
  ],
  templateUrl: './register-component.html',
  styleUrl: './register-component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class RegisterComponent {
  private readonly fb = inject(FormBuilder);
  protected readonly auth = inject(AuthService);
  protected readonly lang = inject(LangService);
  private readonly messages = inject(MessageService);
  private readonly router = inject(Router);

  protected readonly submitting = signal(false);
  protected readonly hidePassword = signal(true);
  protected readonly hideConfirm = signal(true);

  protected readonly form = this.fb.nonNullable.group({
    username: ['', [Validators.required, Validators.minLength(3)]],
    email: ['', [Validators.required, Validators.email]],
    password: ['', [Validators.required, Validators.minLength(4)]],
    confirmPassword: ['', [Validators.required, mustMatchPassword]],
  });

  protected submit(): void {
    if (this.form.invalid) {
      this.form.markAllAsTouched();
      return;
    }
    if (this.submitting()) {
      return;
    }

    this.submitting.set(true);
    const { username, email, password } = this.form.getRawValue();
    this.auth.register({ username, email, password }).subscribe({
      next: () => {
        this.submitting.set(false);
        // Mock tự đăng nhập sau khi tạo — vào thẳng dashboard.
        this.messages.success('REGISTER_SUCCESS');
        void this.router.navigate(['/dashboard']);
      },
      error: (err: unknown) => {
        this.submitting.set(false);
        const code: AuthErrorCode = err instanceof AuthApiError ? err.code : 'GENERIC';
        this.messages.error(REGISTER_ERROR_KEYS[code]);
      },
    });
  }

  protected togglePasswordVisibility(): void {
    this.hidePassword.update((v) => !v);
  }

  protected toggleConfirmVisibility(): void {
    this.hideConfirm.update((v) => !v);
  }

  protected get usernameControl() {
    return this.form.controls.username;
  }

  protected get emailControl() {
    return this.form.controls.email;
  }

  protected get passwordControl() {
    return this.form.controls.password;
  }

  protected get confirmPasswordControl() {
    return this.form.controls.confirmPassword;
  }
}
