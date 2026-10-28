import { inject, Injectable } from '@angular/core';
import { map, Observable } from 'rxjs';
import { ApiService } from '../api/api.service';
import {
  toLlmOptions,
  toLlmProviderOption,
  type LlmModelsDto,
  type LlmOptions,
  type LlmOptionsDto,
  type LlmProviderOption,
  type LlmProviderOptionDto,
} from './llm.models';

/**
 * Đọc và quản trị danh sách provider/model mà backend đã cấu hình (Đợt 28).
 * Endpoint đọc dùng cho menu chọn model ở chat; nhóm ghi (key/model) CHỈ admin
 * gọi được — backend chặn bằng role trên route, không dựa vào UI ẩn nút.
 *
 * API key đi một chiều: gửi lên để lưu (mã hoá ở server), KHÔNG có lời gọi nào
 * đọc key về. Mọi response đều là LlmProviderOption không chứa key.
 */
@Injectable({ providedIn: 'root' })
export class LlmService {
  private readonly api = inject(ApiService);

  options(): Observable<LlmOptions> {
    return this.api.get<LlmOptionsDto>('/llm/options').pipe(map(toLlmOptions));
  }

  /** Lưu key admin nhập; key trong DB thắng key trong configs/config.json */
  saveKey(slug: string, apiKey: string): Observable<LlmProviderOption> {
    return this.api
      .put<LlmProviderOptionDto>(`/llm/providers/${encodeURIComponent(slug)}/key`, {
        api_key: apiKey,
      })
      .pipe(map(toLlmProviderOption));
  }

  /** Xoá key đã lưu → provider quay về key trong config (nếu có) */
  clearKey(slug: string): Observable<LlmProviderOption> {
    return this.api
      .delete<LlmProviderOptionDto>(`/llm/providers/${encodeURIComponent(slug)}/key`)
      .pipe(map(toLlmProviderOption));
  }

  /** Gọi chính provider để lấy danh sách model, backend lưu lại vào DB */
  loadModels(slug: string): Observable<string[]> {
    return this.api
      .post<LlmModelsDto>(`/llm/providers/${encodeURIComponent(slug)}/models`)
      .pipe(map((dto) => dto.models ?? []));
  }

  /** Chọn model mặc định cho provider (thay model config khi chat bỏ trống) */
  selectModel(slug: string, model: string): Observable<LlmProviderOption> {
    return this.api
      .put<LlmProviderOptionDto>(`/llm/providers/${encodeURIComponent(slug)}/model`, { model })
      .pipe(map(toLlmProviderOption));
  }
}
