import { inject, Injectable } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable, map, switchMap } from 'rxjs';
import { API_BASE_URL } from '../auth/services/auth.interface';
import { unwrap } from './api.helpers';
import { ApiResponse, PresignUploadResponse } from './api.models';

/**
 * Upload 2 bước: xin "vé" presign (multipart, BE sniff 512B đầu file)
 * rồi TỰ PUT thẳng vào kho SeaweedFS — không đi qua backend.
 * PUT phải gửi đúng content_type BE đã chốt (nằm trong chữ ký), sai là 403.
 */
@Injectable({ providedIn: 'root' })
export class UploadsService {
  private readonly http = inject(HttpClient);
  private readonly base = inject(API_BASE_URL);

  presign(file: File): Observable<PresignUploadResponse> {
    const fd = new FormData();
    fd.append('file', file, file.name);
    return this.http
      .post<ApiResponse<PresignUploadResponse>>(`${this.base}/uploads/presign`, fd)
      .pipe(unwrap());
  }

  /** Presign + PUT, trả về vé (key + content_type) để tạo article ngay. */
  upload(file: File): Observable<PresignUploadResponse> {
    return this.presign(file).pipe(
      switchMap((ticket) =>
        this.http.put(ticket.upload_url, file, {
          headers: { 'Content-Type': ticket.content_type },
          responseType: 'text',
        }).pipe(map(() => ticket)),
      ),
    );
  }
}
