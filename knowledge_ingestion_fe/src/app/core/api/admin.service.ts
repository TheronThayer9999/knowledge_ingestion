import { inject, Injectable } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';
import { Observable } from 'rxjs';
import { API_BASE_URL } from '../auth/services/auth.interface';
import { unwrap } from './api.helpers';
import {
  ApiResponse,
  ConsistencyCheckResponse,
  EmbedCachePurgeRequest,
  EmbedCachePurgeResponse,
  EmbedCacheStatsResponse,
  QueueStatusResponse,
  RebuildVectorsRequest,
  ResetScopeRequest,
  ResetScopeResponse,
} from './api.models';

/**
 * Admin API (role >= 1 mới gọi được — BE gate RequireRole(RoleAdmin)).
 * FE ẩn nav/route khi user.role < 1, nhưng vẫn phải handle 403 vì token cũ.
 */
@Injectable({ providedIn: 'root' })
export class AdminService {
  private readonly http = inject(HttpClient);
  private readonly base = inject(API_BASE_URL);

  queue(limit?: number): Observable<QueueStatusResponse> {
    let hp = new HttpParams();
    if (limit) {
      hp = hp.set('limit', String(limit));
    }
    return this.http.get<ApiResponse<QueueStatusResponse>>(`${this.base}/admin/queue`, { params: hp }).pipe(unwrap());
  }

  consistency(): Observable<ConsistencyCheckResponse> {
    return this.http.get<ApiResponse<ConsistencyCheckResponse>>(`${this.base}/admin/consistency`).pipe(unwrap());
  }

  embedCacheStats(): Observable<EmbedCacheStatsResponse> {
    return this.http.get<ApiResponse<EmbedCacheStatsResponse>>(`${this.base}/admin/embed-cache/stats`).pipe(unwrap());
  }

  embedCachePurge(req: EmbedCachePurgeRequest): Observable<EmbedCachePurgeResponse> {
    return this.http.post<ApiResponse<EmbedCachePurgeResponse>>(`${this.base}/admin/embed-cache/purge`, req).pipe(unwrap());
  }

  /** Đưa bài về pending index (Qdrant mất data / đổi cụm). */
  rebuildVectors(req: RebuildVectorsRequest): Observable<ResetScopeResponse> {
    return this.http.post<ApiResponse<ResetScopeResponse>>(`${this.base}/admin/rebuild-vectors`, req).pipe(unwrap());
  }

  /** OCR lại toàn bộ trang đã trích của scope chọn. */
  resetExtract(req: ResetScopeRequest): Observable<ResetScopeResponse> {
    return this.http.post<ApiResponse<ResetScopeResponse>>(`${this.base}/admin/reset-extract`, req).pipe(unwrap());
  }
}
