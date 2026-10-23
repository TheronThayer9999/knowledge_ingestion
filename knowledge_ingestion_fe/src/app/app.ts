import { Component } from '@angular/core';
import { RouterOutlet } from '@angular/router';

/** Vỏ ứng dụng: chỉ còn router-outlet — khung dashboard nằm ở DashboardLayout. */
@Component({
  selector: 'app-root',
  imports: [RouterOutlet],
  templateUrl: './app.html',
  styleUrl: './app.scss',
})
export class App {}
