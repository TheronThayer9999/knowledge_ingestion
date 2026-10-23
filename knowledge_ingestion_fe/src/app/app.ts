import { Component, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatIconModule } from '@angular/material/icon';
import { MatToolbarModule } from '@angular/material/toolbar';
import { RouterOutlet } from '@angular/router';

@Component({
  selector: 'app-root',
  imports: [RouterOutlet, MatToolbarModule, MatCardModule, MatButtonModule, MatIconModule],
  templateUrl: './app.html',
  styleUrl: './app.scss'
})
export class App {
  protected readonly title = signal('Knowledge Ingestion');
  protected readonly isDark = signal(false);

  // Theme M3 theo `color-scheme` nên chỉ cần đổi colorScheme trên <html>
  protected toggleTheme(): void {
    this.isDark.update((dark) => !dark);
    document.documentElement.style.colorScheme = this.isDark() ? 'dark' : 'light';
  }
}
