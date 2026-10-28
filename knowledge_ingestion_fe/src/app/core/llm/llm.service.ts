import { inject, Injectable } from '@angular/core';
import { map, Observable } from 'rxjs';
import { ApiService } from '../api/api.service';
import {
  toLlmOptions,
  toLlmProviderOption,
  type LlmModelsDto,
  type LlmOptions,
  type LlmOptionsDto,
  type LlmProviderForm,
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

  /**
   * Thêm mới hoặc sửa ĐỊNH NGHĨA provider (Đợt 28 nấc 3) — loại, base_url,
   * nhãn, tắt/bật, và API key nếu gửi kèm. Field bỏ trống backend giữ nguyên
   * giá trị cũ. Backend trả lại cả danh sách vì thêm provider làm đổi danh sách.
   */
  saveProvider(slug: string, form: LlmProviderForm): Observable<LlmOptions> {
    return this.api
      .put<LlmOptionsDto>(`/llm/providers/${encodeURIComponent(slug)}`, form)
      .pipe(map(toLlmOptions));
  }

  /**
   * Xoá cài đặt của provider: provider khai trong config quay về đúng file,
   * provider tạo từ UI biến mất hẳn. Backend chặn xoá provider mặc định.
   */
  deleteProvider(slug: string): Observable<LlmOptions> {
    return this.api
      .delete<LlmOptionsDto>(`/llm/providers/${encodeURIComponent(slug)}`)
      .pipe(map(toLlmOptions));
  }
}
