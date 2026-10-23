import { Component, input } from '@angular/core';
import { MatChipsModule } from '@angular/material/chips';
import { MatIconModule } from '@angular/material/icon';
import { SectionTreeNode } from '../../core/api/api.models';

/**
 * Nút cây mục tài liệu — đệ quy tự tham chiếu (con render lại chính
 * component này). Tên mục: Chương/Phần > Mục > Điều/Bài (logic nest ở BE).
 */
@Component({
  selector: 'app-section-tree',
  imports: [MatChipsModule, MatIconModule],
  template: `
    <div class="section-tree__node">
      <div class="section-tree__row">
        <mat-icon class="section-tree__icon">
          {{ node().children.length ? 'folder_open' : 'description' }}
        </mat-icon>
        <span class="section-tree__name">{{ node().name }}</span>
        <span class="section-tree__chunk">chunk #{{ node().from }}</span>
      </div>
      @if (node().summary) {
        <p class="section-tree__summary">{{ node().summary }}</p>
      }
      <div class="section-tree__children">
        @for (child of node().children; track child.name + child.from) {
          <app-section-tree [node]="child" />
        }
      </div>
    </div>
  `,
  styles: [
    `
      .section-tree__row {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        padding-block: var(--space-1);
      }
      .section-tree__icon {
        color: var(--app-primary);
        font-size: 18px;
        width: 18px;
        height: 18px;
      }
      .section-tree__name {
        font-weight: 500;
      }
      .section-tree__chunk {
        color: var(--color-text-secondary);
        font-size: 12px;
      }
      .section-tree__summary {
        margin: 0 0 var(--space-1) var(--space-6);
        color: var(--color-text-secondary);
        font-size: 13px;
      }
      .section-tree__children {
        padding-inline-start: var(--space-5);
        border-inline-start: var(--border-width) solid var(--color-border);
        margin-inline-start: var(--space-2);
      }
    `,
  ],
})
export class SectionTree {
  readonly node = input.required<SectionTreeNode>();
}
