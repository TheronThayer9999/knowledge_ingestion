import { inject, Injectable } from '@angular/core';
import { Observable } from 'rxjs';
import { ApiService } from '../api/api.service';

export interface ScanJobResponse {
  id: number;
  name: string;
  status: string;
  total_pages: number;
  done_pages: number;
  failed_pages: number;
  ocr_target_pages: number;
  has_text_layer: boolean;
  force_ocr: boolean;
  lang: string;
  dpi: number;
  psm: number;
  engine: string;
  progress: number;
  error_code?: string;
  error_message?: string;
  expires_at: string;
  created_at: string;
  output_url?: string;
}

/** Tuỳ chọn OCR của MỘT phiên — phải gửi lúc tạo job vì server lưu vào row
 * (`ocr_jobs.lang/dpi/force_ocr`) rồi worker đọc lại; sửa sau khi tạo là vô nghĩa. */
export interface ScanJobCreateOptions {
  lang?: string;
  dpi?: number;
  forceOcr?: boolean;
}

export interface ScanJobDownloadResponse {
  url: string;
  expires_at: string;
  filename: string;
}

@Injectable({ providedIn: 'root' })
export class ScanJobService {
  private readonly api = inject(ApiService);

  create(file: File, options: ScanJobCreateOptions = {}): Observable<ScanJobResponse> {
    const form = new FormData();
    form.append('file', file, file.name);
    if (options.lang) {
      form.append('lang', options.lang);
    }
    if (options.dpi) {
      form.append('dpi', String(options.dpi));
    }
    if (options.forceOcr) {
      form.append('force_ocr', 'true');
    }
    return this.api.post<ScanJobResponse>('/scan/jobs', form);
  }

  get(id: number): Observable<ScanJobResponse> {
    return this.api.get<ScanJobResponse>(`/scan/jobs/${id}`);
  }

  list(
    status = '',
    limit = 20,
    offset = 0,
  ): Observable<{ items: ScanJobResponse[]; total: number; limit: number; offset: number }> {
    const query: Record<string, string | number> = { limit, offset };
    if (status) query['status'] = status;
    return this.api.get<{ items: ScanJobResponse[]; total: number; limit: number; offset: number }>(
      '/scan/jobs',
      query,
    );
  }

  cancel(id: number): Observable<{ id: number; status: string; pages_affected: number }> {
    return this.api.post<{ id: number; status: string; pages_affected: number }>(
      `/scan/jobs/${id}/cancel`,
      {},
    );
  }

  delete(id: number): Observable<{ id: number; status: string; pages_affected: number }> {
    return this.api.delete<{ id: number; status: string; pages_affected: number }>(
      `/scan/jobs/${id}`,
    );
  }

  retryPage(
    id: number,
    page: number,
  ): Observable<{ id: number; status: string; pages_affected: number }> {
    return this.api.post<{ id: number; status: string; pages_affected: number }>(
      `/scan/jobs/${id}/pages/${page}/retry`,
      {},
    );
  }

  download(id: number): Observable<ScanJobDownloadResponse> {
    return this.api.get<ScanJobDownloadResponse>(`/scan/jobs/${id}/download`);
  }
}
