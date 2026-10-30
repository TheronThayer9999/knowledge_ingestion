import { DOCUMENT } from '@angular/common';
import { computed, inject, Injectable, OnDestroy, signal } from '@angular/core';
import { DomSanitizer, type SafeResourceUrl } from '@angular/platform-browser';
import { LangService, type TranslationKey } from '../../core/i18n/lang.service';
import {
  type ScanJobPageResponse,
  type ScanJobResponse,
  ScanJobService,
} from '../../core/scan/scan-job.service';
import { MessageService } from '../../shared/feedback/message.service';

/** Tệp người dùng vừa chọn. `error` là KEY i18n, không phải câu đã dịch — đổi ngôn ngữ vẫn đúng. */
export interface ScanSelectedFile {
  file: File;
  error?: TranslationKey;
}

export type ScanOcrLang = 'vie' | 'eng' | 'vie+eng';
export type ScanMode = 'auto' | 'force';

interface ScanSettings {
  lang: ScanOcrLang;
  dpi: number;
  mode: ScanMode;
}

const OCR_LANGS: readonly ScanOcrLang[] = ['vie', 'eng', 'vie+eng'];
const DPI_OPTIONS: readonly number[] = [150, 200, 300];
const MAX_BYTES = 50 * 1024 * 1024;
/** Nhịp poll. 2s (không phải 1s) vì mỗi lượt là N request; job OCR tính bằng chục giây/trang. */
const POLL_MS = 2000;
const SETTINGS_KEY = 'ki.scan.settings';
const RUNNING_STATUSES = ['pending', 'processing'];
const HISTORY_PAGE_SIZE = 10;

/**
 * Trạng thái dùng chung của 4 tab tab Quét (Tài liệu / Xem & sửa / PDF chuẩn /
 * Lịch sử). Cấp ở trang cha (`providers`) nên rời trang là mất, không rò rỉ.
 *
 * VÌ SAO MỘT STORE CHỨ KHÔNG PHẢI MỖI TAB TỰ GỌI API: 4 tab nhìn cùng một phiên
 * (đang OCR → xem trang → tải PDF → lịch sử), nếu mỗi tab tự poll thì một phiên
 * bị hỏi N lần mỗi nhịp. Ở đây chỉ có MỘT vòng poll cho tất cả phiên đang chạy,
 * và nó dừng khi tab trình duyệt bị ẩn — người dùng mở 10 tab không tạo 10 luồng
 * request nền.
 */
@Injectable()
export class ScanJobStore implements OnDestroy {
  private readonly svc = inject(ScanJobService);
  private readonly messages = inject(MessageService);
  private readonly sanitizer = inject(DomSanitizer);
  private readonly document = inject(DOCUMENT);
  readonly lang = inject(LangService);

  readonly ocrLangs = OCR_LANGS;
  readonly dpiOptions = DPI_OPTIONS;

  private readonly stored = readSettings();
  readonly ocrLang = signal<ScanOcrLang>(this.stored.lang);
  readonly dpi = signal<number>(this.stored.dpi);
  readonly mode = signal<ScanMode>(this.stored.mode);

  /** 5 phiên mới nhất theo server — nguồn cho danh sách "Phiên gần đây". */
  private readonly recentJobs = signal<ScanJobResponse[]>([]);
  /** Trạng thái sống của những phiên trang này biết (vừa tạo hoặc vừa mở). */
  private readonly jobs = signal<ReadonlyMap<number, ScanJobResponse>>(new Map());
  /** Phiên đang mở trong tab Xem & sửa / PDF chuẩn. */
  private readonly active = signal<number | null>(null);

  /** Tab đang mở (0 Tài liệu, 1 Xem & sửa, 2 PDF chuẩn, 3 Lịch sử) — tab con đổi được. */
  readonly tabIndex = signal(0);

  private readonly submitting = signal(false);
  private readonly loadingRecentSignal = signal(false);
  private readonly failure = signal<string | null>(null);
  private readonly notice = signal<string | null>(null);
  private readonly picked = signal<ScanSelectedFile | null>(null);
  readonly dragging = signal(false);
  readonly fullscreen = signal(false);
  readonly showAdvanced = signal(false);

  // ---- Tab Xem & sửa ----
  readonly reviewPage = signal(1);
  private readonly pageData = signal<ScanJobPageResponse | null>(null);
  private readonly pageLoading = signal(false);
  private readonly pageError = signal<string | null>(null);

  // ---- Tab Lịch sử ----
  readonly historyFilter = signal('');
  private readonly history = signal<ScanJobResponse[]>([]);
  private readonly historyTotalSignal = signal(0);
  readonly historyOffset = signal(0);
  private readonly historyLoading = signal(false);

  private timer: ReturnType<typeof setInterval> | null = null;
  private readonly onVisible = (): void => {
    if (!this.document.hidden) {
      this.tick();
    }
  };

  // ---- Bản đọc cho template ----
  readonly file = this.picked.asReadonly();
  readonly creating = this.submitting.asReadonly();
  readonly error = this.failure.asReadonly();
  readonly success = this.notice.asReadonly();
  readonly loadingRecent = this.loadingRecentSignal.asReadonly();
  readonly page = this.pageData.asReadonly();
  readonly pageBusy = this.pageLoading.asReadonly();
  readonly pageFailure = this.pageError.asReadonly();
  readonly historyItems = this.history.asReadonly();
  readonly historyTotal = this.historyTotalSignal.asReadonly();
  readonly historyBusy = this.historyLoading.asReadonly();

  readonly activeJob = computed<ScanJobResponse | null>(() => {
    const id = this.active();
    if (id === null) {
      return null;
    }
    return this.jobs().get(id) ?? this.recentJobs().find((job) => job.id === id) ?? null;
  });

  /** Phiên đang chạy mà trang đang theo dõi — chỉ khi đó mới hiện khối tiến độ. */
  readonly busyJob = computed<ScanJobResponse | null>(() => {
    const job = this.activeJob();
    return job && isRunning(job) ? job : null;
  });

  /**
   * Danh sách hiển thị: 5 phiên của server + phiên vừa tạo trong trang (server
   * có thể chưa kịp trả về), trạng thái lấy bản mới nhất giữa hai nguồn.
   */
  readonly rail = computed<ScanJobResponse[]>(() => {
    const live = this.jobs();
    const fromServer = this.recentJobs();
    const seen = new Set(fromServer.map((job) => job.id));
    const fresh = [...live.values()].filter((job) => !seen.has(job.id));
    return [...fresh.reverse(), ...fromServer.map((job) => live.get(job.id) ?? job)];
  });

  private readonly runningIds = computed<number[]>(() =>
    [...this.jobs().values()].filter(isRunning).map((job) => job.id),
  );

  readonly hasFile = computed(() => this.picked() !== null);
  readonly canConfigure = computed(() => this.hasFile() && !this.submitting());
  readonly canStart = computed(() => {
    const picked = this.picked();
    return !!picked && !picked.error && !this.submitting();
  });
  readonly canDownload = computed(() => this.activeJob()?.status === 'done');
  readonly totalPages = computed(() => this.activeJob()?.total_pages ?? 0);

  /** Vì sao nút "Bắt đầu OCR" đang tắt — nút disabled mà không nói lý do là lỗi UX. */
  readonly startHint = computed<TranslationKey | null>(() => {
    if (this.submitting()) {
      return null;
    }
    const picked = this.picked();
    if (!picked) {
      return 'SCAN_JOBS_HINT_NO_FILE';
    }
    return picked.error ? 'SCAN_JOBS_HINT_FILE_INVALID' : null;
  });

  readonly safeOutputUrl = computed<SafeResourceUrl | null>(() => {
    const url = this.activeJob()?.output_url;
    return url ? this.sanitizer.bypassSecurityTrustResourceUrl(url) : null;
  });

  readonly historyPage = computed(() => Math.floor(this.historyOffset() / HISTORY_PAGE_SIZE) + 1);
  readonly historyPages = computed(() =>
    Math.max(1, Math.ceil(this.historyTotalSignal() / HISTORY_PAGE_SIZE)),
  );
  readonly historyFrom = computed(() => this.historyOffset() + 1);
  readonly historyTo = computed(() =>
    Math.min(this.historyOffset() + HISTORY_PAGE_SIZE, this.historyTotalSignal()),
  );

  /** Gọi một lần từ trang cha: nạp danh sách + bật vòng poll. */
  start(): void {
    this.loadRecent();
    this.loadHistory(true);
    this.document.addEventListener('visibilitychange', this.onVisible);
  }

  ngOnDestroy(): void {
    this.stopWatching();
    this.document.removeEventListener('visibilitychange', this.onVisible);
  }

  // ---- Tệp ----

  /** Chuyển tab; rời tab PDF thì thoát chế độ toàn màn hình cho khỏi kẹt overlay. */
  showTab(index: number): void {
    this.tabIndex.set(index);
    if (index !== 2) {
      this.fullscreen.set(false);
    }
  }

  onFilesSelected(event: Event): void {
    const input = event.target as HTMLInputElement;
    const [picked] = Array.from(input.files ?? []);
    input.value = '';
    if (picked) {
      this.accept(picked);
    }
  }

  onDrop(event: DragEvent): void {
    event.preventDefault();
    this.dragging.set(false);
    const [picked] = Array.from(event.dataTransfer?.files ?? []);
    if (picked) {
      this.accept(picked);
    }
  }

  onDragOver(event: DragEvent): void {
    event.preventDefault();
    this.dragging.set(true);
  }

  onDragLeave(): void {
    this.dragging.set(false);
  }

  removeFile(): void {
    this.picked.set(null);
    this.failure.set(null);
    this.notice.set(null);
  }

  toggleAdvanced(): void {
    this.showAdvanced.update((value) => !value);
  }

  /** Mỗi lượt một tệp (batch là V2): chọn tệp mới thay tệp cũ, không âm thầm bỏ tệp. */
  private accept(picked: File): void {
    this.picked.set({ file: picked, error: validate(picked) });
    this.failure.set(null);
    this.notice.set(null);
  }

  formatBytes(bytes: number): string {
    if (bytes < 1024) {
      return `${bytes} B`;
    }
    if (bytes < 1024 * 1024) {
      return `${(bytes / 1024).toFixed(1)} KB`;
    }
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  }

  fileIcon(picked: File): string {
    return /\.pdf$/i.test(picked.name) ? 'picture_as_pdf' : 'image';
  }

  // ---- Cài đặt ----

  setLang(value: ScanOcrLang): void {
    this.ocrLang.set(value);
    this.persistSettings();
  }

  setDpi(value: number): void {
    this.dpi.set(value);
    this.persistSettings();
  }

  setMode(value: ScanMode): void {
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

  startJob(): void {
    const picked = this.picked();
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
          this.picked.set(null);
          this.open(job);
          this.loadRecent();
          this.loadHistory(true);
        },
        error: (cause: unknown) => {
          this.submitting.set(false);
          this.failure.set(messageOf(cause, this.lang.t('SCAN_JOBS_CREATE_FAILED')));
        },
      });
  }

  cancel(): void {
    const job = this.activeJob();
    if (!job) {
      return;
    }
    this.svc.cancel(job.id).subscribe({
      next: () => {
        this.notice.set(this.lang.t('SCAN_JOBS_CANCEL_DONE'));
        this.refresh(job.id);
        this.loadHistory(true);
      },
      error: (cause: unknown) =>
        this.failure.set(messageOf(cause, this.lang.t('SCAN_JOBS_CANCEL_FAILED'))),
    });
  }

  download(): void {
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

  openInTab(): void {
    const url = this.activeJob()?.output_url;
    if (url) {
      window.open(url, '_blank', 'noopener');
    }
  }

  toggleFullscreen(): void {
    this.fullscreen.update((value) => !value);
  }

  /** Mở một phiên (từ danh sách gần đây hoặc lịch sử) và nạp trang đầu của nó. */
  open(job: ScanJobResponse): void {
    this.merge(job);
    this.active.set(job.id);
    this.reviewPage.set(1);
    this.pageData.set(null);
    this.pageError.set(null);
    if (job.status === 'failed') {
      this.failure.set(job.error_message || this.lang.t('SCAN_JOBS_FAILED'));
    }
    // Luôn lấy bản mới: URL tải/preview là presigned nên bản trong danh sách có
    // thể đã hết hạn, còn phiên đang chạy thì cần số trang mới nhất.
    this.refresh(job.id);
    this.loadPage(1);
    this.watchRunning();
  }

  /** Bỏ chọn phiên đang xem; các phiên khác vẫn chạy. */
  reset(): void {
    this.picked.set(null);
    this.active.set(null);
    this.pageData.set(null);
    this.failure.set(null);
    this.notice.set(null);
  }

  // ---- Xem & sửa ----

  goToPage(page: number): void {
    const total = this.totalPages();
    if (page < 1 || (total > 0 && page > total) || page === this.reviewPage()) {
      return;
    }
    this.loadPage(page);
  }

  nextPage(): void {
    this.goToPage(this.reviewPage() + 1);
  }

  prevPage(): void {
    this.goToPage(this.reviewPage() - 1);
  }

  /** Tải lại đúng trang đang xem — dùng khi OCR vừa xong hoặc ảnh render hỏng. */
  reloadPage(): void {
    this.loadPage(this.reviewPage());
  }

  loadPage(page: number): void {
    const job = this.activeJob();
    if (!job) {
      return;
    }
    this.reviewPage.set(page);
    this.pageLoading.set(true);
    this.pageError.set(null);
    this.svc.getPage(job.id, page).subscribe({
      next: (data) => {
        // Bỏ kết quả về muộn của trang khác: người dùng đã bấm sang trang mới.
        if (this.reviewPage() === data.page) {
          this.pageData.set(data);
        }
        this.pageLoading.set(false);
      },
      error: (cause: unknown) => {
        this.pageLoading.set(false);
        this.pageError.set(messageOf(cause, this.lang.t('SCAN_REVIEW_LOAD_FAILED')));
      },
    });
  }

  retryPage(page: number): void {
    const job = this.activeJob();
    if (!job) {
      return;
    }
    this.svc.retryPage(job.id, page).subscribe({
      next: () => {
        this.messages.success('SCAN_REVIEW_RETRY_DONE');
        this.loadPage(page);
      },
      error: (cause: unknown) => this.messages.errorFromApi(cause, 'SCAN_REVIEW_RETRY_FAILED'),
    });
  }

  copyText(text: string): void {
    void navigator.clipboard?.writeText(text).then(
      () => this.messages.success('SCAN_REVIEW_COPIED'),
      () => this.messages.error('SCAN_REVIEW_COPY_FAILED'),
    );
  }

  // ---- Lịch sử ----

  setHistoryFilter(status: string): void {
    this.historyFilter.set(status);
    this.loadHistory(true);
  }

  loadHistory(reset = false): void {
    const offset = reset ? 0 : this.historyOffset();
    this.historyLoading.set(true);
    this.historyOffset.set(offset);
    this.svc.list(this.historyFilter(), HISTORY_PAGE_SIZE, offset).subscribe({
      next: (page) => {
        this.history.set(page.items);
        this.historyTotalSignal.set(page.total);
        this.historyLoading.set(false);
      },
      error: (cause: unknown) => {
        this.historyLoading.set(false);
        this.messages.errorFromApi(cause, 'SCAN_HISTORY_LOAD_FAILED');
      },
    });
  }

  historyPrev(): void {
    if (this.historyOffset() > 0) {
      this.loadHistoryOffset(Math.max(0, this.historyOffset() - HISTORY_PAGE_SIZE));
    }
  }

  historyNext(): void {
    if (this.historyTo() < this.historyTotalSignal()) {
      this.loadHistoryOffset(this.historyOffset() + HISTORY_PAGE_SIZE);
    }
  }

  private loadHistoryOffset(offset: number): void {
    this.historyOffset.set(offset);
    this.loadHistory(false);
  }

  /** Xoá một phiên khỏi lịch sử: hỏi trước vì bản quét (và PDF đã dựng) mất hẳn. */
  async remove(job: ScanJobResponse): Promise<void> {
    const ok = await this.messages.confirm(
      'SCAN_HISTORY_DELETE_CONFIRM_TITLE',
      'SCAN_HISTORY_DELETE_CONFIRM_MESSAGE',
    );
    if (!ok) {
      return;
    }
    this.svc.delete(job.id).subscribe({
      next: () => {
        this.messages.success('SCAN_HISTORY_DELETED');
        if (this.active() === job.id) {
          this.active.set(null);
          this.pageData.set(null);
        }
        this.jobs.update((known) => {
          const next = new Map(known);
          next.delete(job.id);
          return next;
        });
        this.loadRecent();
        this.loadHistory(true);
      },
      error: (cause: unknown) => this.messages.errorFromApi(cause, 'SCAN_HISTORY_DELETE_FAILED'),
    });
  }

  // ---- Theo dõi tiến độ ----

  /** Bật vòng poll nếu có phiên đang chạy; tắt khi không còn phiên nào. */
  private watchRunning(): void {
    if (this.runningIds().length === 0) {
      this.stopWatching();
      return;
    }
    if (!this.timer) {
      this.timer = setInterval(() => this.tick(), POLL_MS);
    }
  }

  /** Một nhịp poll cho MỌI phiên đang chạy, không chỉ phiên đang xem. */
  private tick(): void {
    if (this.document.hidden) {
      return;
    }
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
    const wasRunning = !!known && isRunning(known);
    this.merge(job);
    if (!isRunning(job) && wasRunning) {
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
    } else if (job.status === 'failed') {
      this.failure.set(job.error_message || this.lang.t('SCAN_JOBS_FAILED'));
    }
    if (this.active() === job.id) {
      this.loadPage(this.reviewPage());
    }
    this.loadRecent();
    this.loadHistory(true);
  }

  private loadRecent(): void {
    this.loadingRecentSignal.set(true);
    this.svc.list('', 5, 0).subscribe({
      // Không có lịch sử không phải lỗi — im lặng để trang trống vẫn sạch.
      next: (page) => {
        this.recentJobs.set(page.items);
        this.loadingRecentSignal.set(false);
      },
      error: () => {
        this.loadingRecentSignal.set(false);
      },
    });
  }

  private stopWatching(): void {
    if (this.timer) {
      clearInterval(this.timer);
      this.timer = null;
    }
  }

  // ---- Nhãn ----

  recentMeta(job: ScanJobResponse): string {
    return `${job.done_pages}/${job.total_pages} ${this.lang.t('SCAN_JOBS_PAGES')}`;
  }

  isRunning(job: ScanJobResponse | null): boolean {
    return !!job && isRunning(job);
  }

  langLabel(code: ScanOcrLang): TranslationKey {
    switch (code) {
      case 'vie':
        return 'SCAN_JOBS_LANG_VI';
      case 'eng':
        return 'SCAN_JOBS_LANG_EN';
      default:
        return 'SCAN_JOBS_LANG_BOTH';
    }
  }

  dpiLabel(value: number): TranslationKey {
    switch (value) {
      case 150:
        return 'SCAN_JOBS_DPI_150';
      case 300:
        return 'SCAN_JOBS_DPI_300';
      default:
        return 'SCAN_JOBS_DPI_200';
    }
  }

  statusKey(status: string): TranslationKey {
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

  /** Màu box một từ theo độ tự tin — ngưỡng 70 khớp `scan_ocr.min_conf_word` của BE. */
  wordTone(word: { c: number }): 'ok' | 'low' {
    return word.c >= 70 ? 'ok' : 'low';
  }
}

function isRunning(job: ScanJobResponse): boolean {
  return RUNNING_STATUSES.includes(job.status);
}

function validate(picked: File): TranslationKey | undefined {
  // WEBP cố ý không nhận: kết quả của tab là tệp PDF mà BE chỉ nhúng được
  // JPEG/PNG — nhận webp thì không có đường nào sinh PDF đúng.
  if (!/\.(pdf|png|jpe?g)$/i.test(picked.name)) {
    return 'SCAN_JOBS_INVALID_TYPE';
  }
  if (picked.size > MAX_BYTES) {
    return 'SCAN_JOBS_TOO_LARGE';
  }
  return undefined;
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
      lang: OCR_LANGS.includes(stored.lang as ScanOcrLang)
        ? (stored.lang as ScanOcrLang)
        : fallback.lang,
      dpi: DPI_OPTIONS.includes(stored.dpi ?? -1) ? (stored.dpi as number) : fallback.dpi,
      mode: stored.mode === 'force' ? 'force' : 'auto',
    };
  } catch {
    return fallback;
  }
}
