import { Component, inject } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatMenuModule } from '@angular/material/menu';
import { LANGS, LangService } from '../../core/i18n/lang.service';

@Component({
  selector: 'app-language-switcher',
  imports: [MatButtonModule, MatIconModule, MatMenuModule],
  templateUrl: './language-switcher.html',
  styleUrl: './language-switcher.scss'
})
export class LanguageSwitcher {
  protected readonly lang = inject(LangService);
  protected readonly languages = LANGS;

  protected labelKey(language: (typeof LANGS)[number]): 'LANG_VI' | 'LANG_EN' {
    return language === 'vi' ? 'LANG_VI' : 'LANG_EN';
  }
}
