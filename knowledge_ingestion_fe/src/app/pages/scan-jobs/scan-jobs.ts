import { DOCUMENT } from '@angular/common';
import { Component, computed, inject, signal, OnDestroy } from '@angular/core';
import { DomSanitizer, SafeResourceUrl } from '@angular/platform-browser';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { MatSelectModule } from '@angular/material/select';
import { MatSnackBar } from '@angular/material/snack-bar';
import { MatTooltipModule } from '@angular/material/tooltip';
import { LangService, type TranslationKey } from '../../core/i18n/lang.service';
import { ScanJobResponse, ScanJobService } from '../../core/scan/scan-job.service';

interface SelectedFile {
  file: File;
  /** Key i18n của lỗi validate — template dịch, không lưu câu đã dịch (đổi ngôn ngữ vẫn đúng). */
  error?: TranslationKey;
}

interface ScanSettings {
  lang: OcrLang;
  dpi: number;
  mode: ScanMode;
}

type OcrLang = 'vie' | 'eng' | 'vie+eng';
type ScanMode = 'auto' | 'force';

const OCR_LANGS: readonly OcrLang[] = ['vie', 'eng', 'vie+eng'];
const DPI_OPTIONS: readonly number[] = [150, 200, 300];
const MAX_BYTES = 50 * 1024 * 1024;
const POLL_MS = 2000;
const SETTINGS_KEY = 'ki.scan.settings';
const RUNNING_STATUSES = ['pending', 'processing'];

/**
 * Trang Quét tài liệu → PDF searchable.
 *
 * Ba khối theo đúng thứ tự thao tác (tệp → cài đặt → chạy) + một khung xem PDF.
 * Cài đặt OCR phải đi cùng request tạo job (`ScanJobService.create`) vì server
 * lưu vào row rồi worker đọc lại — trước đây form có mà không gửi, đổi cài đặt
 * không có tác dụng gì.
 */
@Component({
  selector: 'app-scan-jobs',
  imports: [
    MatButtonModule,
    MatFormFieldModule,
    MatIconModule,
    MatProgressBarModule,
    MatSelectModule,
    MatTooltipModule,
  ],
  templateUrl: './scan-jobs.html',
  styleUrl: './scan-jobs.scss',
})
export class ScanJobs implements OnDestroy {
  private readonly svc = inject(ScanJobService);
  private readonly sanitizer = inject(DomSanitizer);
  private readonly snack = inject(MatSnackBar);
  private readonly document = inject(DOCUMENT);
  protected readonly lang = inject(LangService);

  protected readonly ocrLangs = OCR_LANGS;
  protected readonly dpiOptions = DPI_OPTIONS;

  private readonly stored = readSettings();
  protected readonly ocrLang = signal<OcrLang>(this.stored.lang);
  protected readonly dpi = signal<number>(this.stored.dpi);
  protected readonly mode = signal<ScanMode>(this.stored.mode);

  protected readonly recent = signal<ScanJobResponse[]>([]);

  /** Trạng thái kéo-thả đang phủ lên vùng thả tệp. */
  protected readonly dragging = signal(false);
  /** Xem PDF ở chế độ toàn màn hình (chỉ khung xem, không cả trang). */
  protected readonly fullscreen = signal(false);

  private readonly current = signal<SelectedFile | null>(null);
  private readonly job = signal<ScanJobResponse | null>(null);
  private readonly previewUrl = signal<string | null>(null);
  private readonly failure = signal<string | null>(null);
  private readonly notice = signal<string | null>(null);
  private readonly working = signal(false);

  private timer: ReturnType<typeof setInterval> | null = null;

  protected readonly file = this.current.asReadonly();
  protected readonly activeJob = this.job.asReadonly();
  protected readonly busy = this.working.asReadonly();
  protected readonly error = this.failure.asReadonly();
  protected readonly success = this.notice.asReadonly();

  protected readonly hasFile = computed(() => this.current() !== null);
  protected readonly canConfigure = computed(() => this.hasFile() && !this.working());
  protected readonly canStart = computed(() => {
    const picked = this.current();
    return !!picked && !picked.error && !this.working();
  });
  protected readonly canDownload = computed(() => this.job()?.status === 'done');

  /** Vì sao nút "Bắt đầu OCR" đang tắt — nút disabled mà không nói lý do là lỗi UX. */
  protected readonly startHint = computed<TranslationKey | null>(() => {
    if (this.working()) {
      return null;
    }
    const picked = this.current();
    if (!picked) {
      return 'SCAN_JOBS_HINT_NO_FILE';
    }
    return picked.error ? 'SCAN_JOBS_HINT_FILE_INVALID' : null;
  });

  constructor() {
    this.loadRecent();
  }

  // ---- Tệp ----

  protected onFilesSelected(event: Event): void {
    const input = event.target as HTMLInputElement;
    const [picked] = Array.from(input.files ?? []);
    input.value = '';
    if (picked) {
      this.accept(picked);
    }
  }

  protected onDrop(event: DragEvent): void {
    event.preventDefault();
    this.dragging.set(false);
    const [picked] = Array.from(event.dataTransfer?.files ?? []);
    if (picked) {
      this.accept(picked);
    }
  }

  protected onDragOver(event: DragEvent): void {
    event.preventDefault();
    this.dragging.set(true);
  }

  protected onDragLeave(): void {
    this.dragging.set(false);
  }

  protected removeFile(): void {
    this.current.set(null);
    this.clearResult();
  }

  /** Mỗi lượt một tệp (batch là V2): chọn tệp mới thay tệp cũ, không âm thầm bỏ tệp. */
  private accept(picked: File): void {
    if (this.working()) {
      return;
    }
    this.current.set({ file: picked, error: this.validate(picked) });
    this.clearResult();
  }

  private validate(picked: File): TranslationKey | undefined {
    if (!/\.(pdf|png|jpe?g|webp)$/i.test(picked.name)) {
      return 'SCAN_JOBS_INVALID_TYPE';
    }
    if (picked.size > MAX_BYTES) {
      return 'SCAN_JOBS_TOO_LARGE';
    }
    return undefined;
  }

  protected formatBytes(bytes: number): string {
    if (bytes < 1024) {
      return `${bytes} B`;
    }
    if (bytes < 1024 * 1024) {
      return `${(bytes / 1024).toFixed(1)} KB`;
    }
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  }

  protected fileIcon(picked: File): string {
    return picked.type === 'application/pdf' || /\.pdf$/i.test(picked.name)
      ? 'picture_as_pdf'
      : 'image';
  }

  // ---- Cài đặt ----

  protected setLang(value: OcrLang): void {
    this.ocrLang.set(value);
    this.persistSettings();
  }

  protected setDpi(value: number): void {
    this.dpi.set(value);
    this.persistSettings();
  }

  protected setMode(value: ScanMode): void {
    this.mode.set(value);
    this.persistSettings();
  }

  private persistSettings(): void {
    const next: ScanSettings = { lang: this.ocrLang(), dpi: this.dpi(), mode: this.mode() };
    try {
      localStorage.setItem(SETTINGS_KEY, JSON.stringify(next));
    } catch {
      // localStorage bị chặn (private mode) — cài đặt chỉ mất giữa các lần mở
    }
  }

  // ---- Chạy ----

  protected start(): void {
    const picked = this.current();
    if (!picked || picked.error || this.working()) {
      return;
    }
    this.working.set(true);
    this.failure.set(null);
    this.notice.set(null);
    this.previewUrl.set(null);
    this.job.set(null);

    this.svc
      .create(picked.file, {
        lang: this.ocrLang(),
        dpi: this.dpi(),
        forceOcr: this.mode() === 'force',
      })
      .subscribe({
        next: (job) => {
          this.job.set(job);
          if (job.output_url) {
            this.previewUrl.set(job.output_url);
          }
          this.watch(job.id);
          this.loadRecent();
        },
        error: (cause: unknown) => {
          this.working.set(false);
          this.failure.set(messageOf(cause, this.lang.t('SCAN_JOBS_CREATE_FAILED')));
        },
      });
  }

  private watch(id: number): void {
    this.stopWatching();
    this.timer = setInterval(() => this.refresh(id), POLL_MS);
  }

  private refresh(id: number): void {
    this.svc.get(id).subscribe({
      next: (job) => this.apply(job),
      // Lỗi mạng lẻ không được giết tiến trình: job vẫn chạy ở worker, poll tiếp.
      error: (cause: unknown) =>
        this.failure.set(messageOf(cause, this.lang.t('SCAN_JOBS_POLL_ERROR'))),
    });
  }

  private apply(job: ScanJobResponse): void {
    this.job.set(job);
    if (job.output_url) {
      this.previewUrl.set(job.output_url);
    }
    if (RUNNING_STATUSES.includes(job.status)) {
      return;
    }
    this.stopWatching();
    this.working.set(false);
    if (job.status === 'done') {
      this.notice.set(this.lang.t('SCAN_JOBS_SUCCESS'));
      this.announceInBackground();
    } else if (job.status === 'failed') {
      this.failure.set(job.error_message || this.lang.t('SCAN_JOBS_FAILED'));
    }
    this.loadRecent();
  }

  /** Tab đang bị che thì mới toast — đang nhìn thì banner trong trang là đủ. */
  private announceInBackground(): void {
    if (!this.document.hidden) {
      return;
    }
    this.snack.open(this.lang.t('SCAN_JOBS_DONE_TOAST'), this.lang.t('SCAN_JOBS_CLOSE'), {
      duration: 5000,
    });
  }

  protected cancel(): void {
    const job = this.job();
    if (!job) {
      return;
    }
    this.svc.cancel(job.id).subscribe({
      next: () => {
        this.stopWatching();
        this.working.set(false);
        this.notice.set(this.lang.t('SCAN_JOBS_CANCEL_DONE'));
      },
      error: (cause: unknown) =>
        this.failure.set(messageOf(cause, this.lang.t('SCAN_JOBS_CANCEL_FAILED'))),
    });
  }

  protected download(): void {
    const job = this.job();
    if (!job) {
      return;
    }
    this.svc.download(job.id).subscribe({
      next: (file) => window.open(file.url, '_blank', 'noopener'),
      error: (cause: unknown) =>
        this.failure.set(messageOf(cause, this.lang.t('SCAN_JOBS_DOWNLOAD_FAILED'))),
    });
  }

  protected openInTab(): void {
    const url = this.previewUrl();
    if (url) {
      window.open(url, '_blank', 'noopener');
    }
  }

  protected reset(): void {
    this.stopWatching();
    this.current.set(null);
    this.working.set(false);
    this.clearResult();
    this.loadRecent();
  }

  private clearResult(): void {
    this.stopWatching();
    this.job.set(null);
    this.previewUrl.set(null);
    this.failure.set(null);
    this.notice.set(null);
  }

  // ---- Lịch sử gần đây ----

  private loadRecent(): void {
    this.svc.list('', 5, 0).subscribe({
      // Không có lịch sử không phải lỗi — im lặng để trang trống vẫn sạch.
      next: (page) => this.recent.set(page.items),
      error: () => undefined,
    });
  }

  protected openRecent(job: ScanJobResponse): void {
    this.clearResult();
    this.current.set(null);
    this.working.set(RUNNING_STATUSES.includes(job.status));
    this.svc.get(job.id).subscribe({
      next: (fresh) => {
        this.apply(fresh);
        if (RUNNING_STATUSES.includes(fresh.status)) {
          this.watch(fresh.id);
        }
      },
      error: (cause: unknown) =>
        this.failure.set(messageOf(cause, this.lang.t('SCAN_JOBS_OPEN_FAILED'))),
    });
  }

  protected recentMeta(job: ScanJobResponse): string {
    return `${job.done_pages}/${job.total_pages} ${this.lang.t('SCAN_JOBS_PAGES')}`;
  }

  protected toggleFullscreen(): void {
    this.fullscreen.update((value) => !value);
  }

  protected langLabel(code: OcrLang): TranslationKey {
    switch (code) {
      case 'vie':
        return 'SCAN_JOBS_LANG_VI';
      case 'eng':
        return 'SCAN_JOBS_LANG_EN';
      default:
        return 'SCAN_JOBS_LANG_BOTH';
    }
  }

  protected dpiLabel(value: number): TranslationKey {
    switch (value) {
      case 150:
        return 'SCAN_JOBS_DPI_150';
      case 300:
        return 'SCAN_JOBS_DPI_300';
      default:
        return 'SCAN_JOBS_DPI_200';
    }
  }

  // ---- Trạng thái hiển thị ----

  protected statusKey(status: string): TranslationKey {
    switch (status) {
      case 'done':
        return 'SCAN_JOBS_STATUS_DONE';
      case 'failed':
        return 'SCAN_JOBS_STATUS_FAILED';
      case 'cancelled':
        return 'SCAN_JOBS_STATUS_CANCELLED';
      case 'processing':
        return 'SCAN_JOBS_STATUS_PROCESSING';
      default:
        return 'SCAN_JOBS_STATUS_PENDING';
    }
  }

  protected safePreviewUrl(): SafeResourceUrl | null {
    const url = this.previewUrl();
    return url ? this.sanitizer.bypassSecurityTrustResourceUrl(url) : null;
  }

  ngOnDestroy(): void {
    this.stopWatching();
  }

  private stopWatching(): void {
    if (this.timer) {
      clearInterval(this.timer);
      this.timer = null;
    }
  }
}

/** Lỗi BE đã là câu tiếng Việt (envelope `message`); chỉ thay bằng câu mặc định khi trống. */
function messageOf(cause: unknown, fallback: string): string {
  const message = (cause as { message?: string } | null)?.message;
  return message && message.trim() ? message : fallback;
}

function readSettings(): ScanSettings {
  const fallback: ScanSettings = { lang: 'vie+eng', dpi: 200, mode: 'auto' };
  try {
    const raw = localStorage.getItem(SETTINGS_KEY);
    if (!raw) {
      return fallback;
    }
    const stored = JSON.parse(raw) as Partial<ScanSettings>;
    return {
      lang: OCR_LANGS.includes(stored.lang as OcrLang) ? (stored.lang as OcrLang) : fallback.lang,
      dpi: DPI_OPTIONS.includes(stored.dpi ?? -1) ? (stored.dpi as number) : fallback.dpi,
      mode: stored.mode === 'force' ? 'force' : 'auto',
    };
  } catch {
    return fallback;
  }
}
