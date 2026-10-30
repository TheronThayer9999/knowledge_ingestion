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
 *
 * Trạng thái của MỌI phiên nằm trong `jobs` (theo id) chứ không phải một biến
 * `job` duy nhất: worker chạy nhiều phiên song song, nên đang OCR tệp này vẫn
 * phải mở/xem/tải được tệp đã xong mà không làm mất dấu phiên đang chạy.
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

  /** 5 phiên mới nhất theo server — nguồn cho danh sách bên trái. */
  private readonly recent = signal<ScanJobResponse[]>([]);
  /** Trạng thái sống của những phiên trang này biết (vừa tạo hoặc vừa mở). */
  private readonly jobs = signal<ReadonlyMap<number, ScanJobResponse>>(new Map());
  /** Phiên đang hiện trong khung xem — độc lập với việc phiên nào đang chạy. */
  private readonly activeId = signal<number | null>(null);
  /** Request tạo phiên đang bay; chỉ chặn đúng thao tác tạo, không chặn cả trang. */
  private readonly submitting = signal(false);
  private readonly current = signal<SelectedFile | null>(null);
  private readonly failure = signal<string | null>(null);
  private readonly notice = signal<string | null>(null);

  private timer: ReturnType<typeof setInterval> | null = null;

  protected readonly file = this.current.asReadonly();
  protected readonly creating = this.submitting.asReadonly();
  protected readonly error = this.failure.asReadonly();
  protected readonly success = this.notice.asReadonly();

  protected readonly activeJob = computed<ScanJobResponse | null>(() => {
    const id = this.activeId();
    if (id === null) {
      return null;
    }
    return this.jobs().get(id) ?? this.recent().find((job) => job.id === id) ?? null;
  });

  /** Phiên đang chạy mà khung xem đang theo dõi — chỉ khi đó mới hiện khối tiến độ. */
  protected readonly busyJob = computed<ScanJobResponse | null>(() => {
    const job = this.activeJob();
    return job && RUNNING_STATUSES.includes(job.status) ? job : null;
  });

  /**
   * Danh sách hiển thị: 5 phiên của server + phiên vừa tạo trong trang (server
   * có thể chưa kịp trả về), trạng thái lấy bản mới nhất giữa hai nguồn.
   */
  protected readonly rail = computed<ScanJobResponse[]>(() => {
    const live = this.jobs();
    const fromServer = this.recent();
    const seen = new Set(fromServer.map((job) => job.id));
    const fresh = [...live.values()].filter((job) => !seen.has(job.id));
    return [...fresh.reverse(), ...fromServer.map((job) => live.get(job.id) ?? job)];
  });

  private readonly runningIds = computed<number[]>(() =>
    [...this.jobs().values()]
      .filter((job) => RUNNING_STATUSES.includes(job.status))
      .map((job) => job.id),
  );

  protected readonly hasFile = computed(() => this.current() !== null);
  protected readonly canConfigure = computed(() => this.hasFile() && !this.submitting());
  protected readonly canStart = computed(() => {
    const picked = this.current();
    return !!picked && !picked.error && !this.submitting();
  });
  protected readonly canDownload = computed(() => this.activeJob()?.status === 'done');

  /** Vì sao nút "Bắt đầu OCR" đang tắt — nút disabled mà không nói lý do là lỗi UX. */
  protected readonly startHint = computed<TranslationKey | null>(() => {
    if (this.submitting()) {
      return null;
    }
    const picked = this.current();
    if (!picked) {
      return 'SCAN_JOBS_HINT_NO_FILE';
    }
    return picked.error ? 'SCAN_JOBS_HINT_FILE_INVALID' : null;
  });

  protected readonly safePreviewUrl = computed<SafeResourceUrl | null>(() => {
    const url = this.activeJob()?.output_url;
    return url ? this.sanitizer.bypassSecurityTrustResourceUrl(url) : null;
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

  /** Trạng thái kéo-thả đang phủ lên vùng thả tệp. */
  protected readonly dragging = signal(false);
  /** Xem PDF ở chế độ toàn màn hình (chỉ khung xem, không cả trang). */
  protected readonly fullscreen = signal(false);
  /** Đang nạp danh sách phiên gần đây — hiện skeleton thay vì list rỗng. */
  protected readonly loadingRecent = signal(false);
  /** Hiện advanced settings (DPI, Mode) — ẩn mặc định để giảm clutter. */
  protected readonly showAdvanced = signal(false);

  /** Bật/tắt advanced settings (progressive disclosure). */
  protected toggleAdvanced(): void {
    this.showAdvanced.update((value) => !value);
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
    this.failure.set(null);
    this.notice.set(null);
  }

  /** Mỗi lượt một tệp (batch là V2): chọn tệp mới thay tệp cũ, không âm thầm bỏ tệp. */
  private accept(picked: File): void {
    this.current.set({ file: picked, error: this.validate(picked) });
    this.failure.set(null);
    this.notice.set(null);
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
    if (!picked || picked.error || this.submitting()) {
      return;
    }
    this.submitting.set(true);
    this.failure.set(null);
    this.notice.set(null);

    this.svc
      .create(picked.file, {
        lang: this.ocrLang(),
        dpi: this.dpi(),
        forceOcr: this.mode() === 'force',
      })
      .subscribe({
        next: (job) => {
          this.submitting.set(false);
          // Tệp đã thành một phiên: trả bước 1 về trạng thái trống để chọn tệp
          // tiếp theo ngay, không phải chờ phiên này xong.
          this.current.set(null);
          this.activeId.set(job.id);
          this.merge(job);
          this.watchRunning();
          this.loadRecent();
        },
        error: (cause: unknown) => {
          this.submitting.set(false);
          this.failure.set(messageOf(cause, this.lang.t('SCAN_JOBS_CREATE_FAILED')));
        },
      });
  }

  protected cancel(): void {
    const job = this.activeJob();
    if (!job) {
      return;
    }
    this.svc.cancel(job.id).subscribe({
      next: () => {
        this.notice.set(this.lang.t('SCAN_JOBS_CANCEL_DONE'));
        this.refresh(job.id);
      },
      error: (cause: unknown) =>
        this.failure.set(messageOf(cause, this.lang.t('SCAN_JOBS_CANCEL_FAILED'))),
    });
  }

  protected download(): void {
    const job = this.activeJob();
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
    const url = this.activeJob()?.output_url;
    if (url) {
      window.open(url, '_blank', 'noopener');
    }
  }

  /** Bỏ tệp đang chọn + bỏ chọn phiên trong khung xem; các phiên khác vẫn chạy. */
  protected reset(): void {
    this.current.set(null);
    this.activeId.set(null);
    this.failure.set(null);
    this.notice.set(null);
  }

  // ---- Theo dõi tiến độ ----

  /** Một vòng poll cho TẤT CẢ phiên đang chạy, không chỉ phiên đang xem. */
  private watchRunning(): void {
    if (this.runningIds().length === 0) {
      this.stopWatching();
      return;
    }
    if (!this.timer) {
      this.timer = setInterval(() => this.refreshRunning(), POLL_MS);
    }
  }

  private refreshRunning(): void {
    const ids = this.runningIds();
    if (ids.length === 0) {
      this.stopWatching();
      return;
    }
    for (const id of ids) {
      this.refresh(id);
    }
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
    const known = this.jobs().get(job.id);
    const wasRunning = !!known && RUNNING_STATUSES.includes(known.status);
    this.merge(job);
    if (!RUNNING_STATUSES.includes(job.status) && wasRunning) {
      this.settle(job);
    }
    this.watchRunning();
  }

  private merge(job: ScanJobResponse): void {
    this.jobs.update((known) => new Map(known).set(job.id, job));
  }

  /** Phiên vừa rời trạng thái chạy — chỉ lúc này mới báo xong/lỗi cho người dùng. */
  private settle(job: ScanJobResponse): void {
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

  // ---- Danh sách phiên ----

  private loadRecent(): void {
    this.loadingRecent.set(true);
    this.svc.list('', 5, 0).subscribe({
      // Không có lịch sử không phải lỗi — im lặng để trang trống vẫn sạch.
      next: (page) => {
        this.recent.set(page.items);
        this.loadingRecent.set(false);
      },
      error: () => {
        this.loadingRecent.set(false);
      },
    });
  }

  /** Mở một phiên trong khung xem; phiên khác đang chạy vẫn được poll tiếp. */
  protected openRecent(job: ScanJobResponse): void {
    this.activeId.set(job.id);
    this.merge(job);
    if (job.status === 'failed') {
      this.failure.set(job.error_message || this.lang.t('SCAN_JOBS_FAILED'));
    }
    // Luôn lấy bản mới: link tải/preview là presigned nên bản trong danh sách có
    // thể đã hết hạn, còn phiên đang chạy thì cần số trang mới nhất.
    this.refresh(job.id);
    this.watchRunning();
  }

  protected recentMeta(job: ScanJobResponse): string {
    return `${job.done_pages}/${job.total_pages} ${this.lang.t('SCAN_JOBS_PAGES')}`;
  }

  protected isRunning(job: ScanJobResponse | null): boolean {
    return !!job && RUNNING_STATUSES.includes(job.status);
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
