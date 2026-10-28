import { inject, Injectable } from '@angular/core';
import { map, Observable } from 'rxjs';
import { ApiService } from '../api/api.service';
import { toLlmOptions, type LlmOptions, type LlmOptionsDto } from './llm.models';

/**
 * Đọc danh sách provider/model mà backend đã cấu hình (Đợt 28). Endpoint chỉ
 * đọc — không có API trong app này, nên service này là nguồn duy nhất cho cả
 * menu chọn model ở chat lẫn danh sách kết nối ở Settings.
 */
@Injectable({ providedIn: 'root' })
export class LlmService {
  private readonly api = inject(ApiService);

  options(): Observable<LlmOptions> {
    return this.api.get<LlmOptionsDto>('/llm/options').pipe(map(toLlmOptions));
  }
}
