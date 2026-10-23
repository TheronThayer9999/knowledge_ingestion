import { Component, inject, signal } from '@angular/core';
import {
  AbstractControl,
  FormBuilder,
  ReactiveFormsModule,
  ValidationErrors,
  Validators,
} from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatProgressSpinnerModule } from '@angular/material/progress-spinner';
import { Router, RouterLink } from '@angular/router';
import { finalize } from 'rxjs';
import { AuthService } from '../../core/auth/auth.service';
import { LangService } from '../../core/i18n/lang.service';
import { MessageService } from '../../shared/feedback/message.service';
import { LanguageSwitcher } from '../../shared/language-switcher/language-switcher';
import { ThemeSwitcher } from '../../shared/theme-switcher/theme-switcher';

/** Định dạng E.164 (+84901234567) — khớp rule binding "e164" của backend */
const E164_PATTERN = /^\+[1-9]\d{1,14}$/;

/** Confirm phải trùng password — validator gắn ở cấp group */
const passwordsMatch = (group: AbstractControl): ValidationErrors | null =>
  group.get('password')?.value === group.get('confirmPassword')?.value
    ? null
    : { passwordsMismatch: true };

@Component({
  selector: 'app-register',
  imports: [
    ReactiveFormsModule,
    RouterLink,
    MatCardModule,
    MatFormFieldModule,
    MatInputModule,
    MatButtonModule,
    MatIconModule,
    MatProgressSpinnerModule,
    LanguageSwitcher,
    ThemeSwitcher,
  ],
  templateUrl: './register.html',
  styleUrl: './register.scss',
})
export class Register {
  protected readonly lang = inject(LangService);
  protected readonly form;
  readonly loading = signal(false);

  private readonly auth = inject(AuthService);
  private readonly messages = inject(MessageService);
  private readonly router = inject(Router);

  constructor() {
    const fb = inject(FormBuilder);
    this.form = fb.nonNullable.group(
      {
        username: ['', [Validators.required, Validators.minLength(3), Validators.maxLength(50)]],
        email: ['', [Validators.required, Validators.email]],
        phone: ['', [Validators.pattern(E164_PATTERN)]],
        password: ['', [Validators.required, Validators.minLength(8)]],
        confirmPassword: ['', [Validators.required]],
      },
      { validators: passwordsMatch },
    );
  }

  submit(): void {
    if (this.loading()) {
      return;
    }
    if (this.form.invalid) {
      this.form.markAllAsTouched();
      return;
    }
    const { username, email, phone, password } = this.form.getRawValue();
    const trimmedPhone = phone.trim();
    this.loading.set(true);
    this.auth
      .register({
        username: username.trim(),
        email: email.trim(),
        password,
        ...(trimmedPhone ? { phone: trimmedPhone } : {}),
      })
      .pipe(finalize(() => this.loading.set(false)))
      .subscribe({
        next: () => {
          this.messages.success('AUTH_REGISTER_SUCCESS');
          void this.router.navigateByUrl('/');
        },
        error: (error: unknown) => this.messages.errorFromApi(error),
      });
  }
}
