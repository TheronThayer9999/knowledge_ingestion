import { Component, inject, signal } from '@angular/core';
import { RouterOutlet } from '@angular/router';
import { LangService } from './shared/i18n/lang.service';
import { ThemeService } from './shared/theme/theme.service';

@Component({
  selector: 'app-root',
  imports: [RouterOutlet],
  templateUrl: './app.html',
  styleUrl: './app.scss'
})
export class App {
  protected readonly title = signal('knowledge_ingestion_fe');

  // Bootstrap 2 service global ngay từ root: restore theme/language đã lưu khi mở tab,
  // kể cả trang login (không có TopBar nên nếu chờ component khác inject thì theme không kịp áp).
  constructor() {
    inject(ThemeService);
    inject(LangService);
  }
}
