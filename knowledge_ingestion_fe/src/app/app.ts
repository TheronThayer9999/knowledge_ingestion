import { Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatIconModule } from '@angular/material/icon';
import { MatToolbarModule } from '@angular/material/toolbar';
import { RouterOutlet } from '@angular/router';
import { LangService } from './core/i18n/lang.service';
import { LanguageSwitcher } from './shared/language-switcher/language-switcher';
import { ThemeSwitcher } from './shared/theme-switcher/theme-switcher';

@Component({
  selector: 'app-root',
  imports: [
    RouterOutlet,
    MatToolbarModule,
    MatCardModule,
    MatButtonModule,
    MatIconModule,
    ThemeSwitcher,
    LanguageSwitcher,
  ],
  templateUrl: './app.html',
  styleUrl: './app.scss',
})
export class App {
  protected readonly lang = inject(LangService);
}
