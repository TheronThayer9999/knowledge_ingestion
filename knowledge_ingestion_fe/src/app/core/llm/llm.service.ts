import { inject, Injectable } from '@angular/core';
import { map, Observable } from 'rxjs';
import { ApiService } from '../api/api.service';
import {
  toLlmOptions,
  toLlmProviderCatalogItem,
  toLlmProviderOption,
  type LlmModelsDto,
  type LlmOptions,
  type LlmOptionsDto,
  type LlmProviderCatalogItem,
  type LlmProviderCatalogItemDto,
  type LlmProviderForm,
  type LlmProviderOption,
  type LlmProviderOptionDto,
  type LlmUserProviderForm,
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

  // ---- Kết nối RIÊNG của tôi (BYOK) ----
  //
  // Đường này KHÔNG gate role: mỗi tài khoản tự thêm provider của riêng mình.
  // Backend lấy chủ sở hữu từ token nên không có tham số user_id nào ở đây.
  // API key vẫn đi MỘT chiều: gửi lên để lưu, không có lời gọi nào đọc về.

  /** Danh sách kết nối riêng của tôi — màn Cài đặt dùng để vẽ danh sách. */
  listMyProviders(): Observable<LlmProviderOption[]> {
    return this.api
      .get<LlmProviderOptionDto[]>('/llm/my/providers')
      .pipe(map((list) => (list ?? []).map(toLlmProviderOption)));
  }

  /**
   * Danh mục provider hệ thống mà tôi được gắn key riêng. Chỉ có nhãn +
   * giao thức — không có base URL nội bộ và không có key.
   */
  catalog(): Observable<LlmProviderCatalogItem[]> {
    return this.api
      .get<LlmProviderCatalogItemDto[]>('/llm/my/providers/catalog')
      .pipe(map((list) => (list ?? []).map(toLlmProviderCatalogItem)));
  }

  /** Tạo mới (id = 0) hoặc sửa (id > 0) một kết nối riêng. */
  saveMyProvider(id: number, form: LlmUserProviderForm): Observable<LlmProviderOption> {
    const request$ =
      id > 0
        ? this.api.put<LlmProviderOptionDto>(`/llm/my/providers/${id}`, form)
        : this.api.post<LlmProviderOptionDto>('/llm/my/providers', form);
    return request$.pipe(map(toLlmProviderOption));
  }

  /** Xoá một kết nối riêng — backend trả lại danh sách còn lại. */
  deleteMyProvider(id: number): Observable<LlmProviderOption[]> {
    return this.api
      .delete<LlmProviderOptionDto[]>(`/llm/my/providers/${id}`)
      .pipe(map((list) => (list ?? []).map(toLlmProviderOption)));
  }

  /** Gọi chính provider của tôi bằng key của tôi để lấy danh sách model. */
  loadMyProviderModels(id: number): Observable<string[]> {
    return this.api
      .post<LlmModelsDto>(`/llm/my/providers/${id}/models`)
      .pipe(map((dto) => dto.models ?? []));
  }
}
