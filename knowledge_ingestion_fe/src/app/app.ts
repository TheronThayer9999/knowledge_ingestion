import { Component, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatIconModule } from '@angular/material/icon';
import { MatToolbarModule } from '@angular/material/toolbar';
import { RouterOutlet } from '@angular/router';
import { ThemeSwitcher } from './shared/theme-switcher/theme-switcher';

@Component({
  selector: 'app-root',
  imports: [RouterOutlet, MatToolbarModule, MatCardModule, MatButtonModule, MatIconModule, ThemeSwitcher],
  templateUrl: './app.html',
  styleUrl: './app.scss'
})
export class App {
  protected readonly title = signal('Knowledge Ingestion');
}
