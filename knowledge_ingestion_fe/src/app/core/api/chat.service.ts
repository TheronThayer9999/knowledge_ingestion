import { inject, Injectable } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable } from 'rxjs';
import { API_BASE_URL } from '../auth/services/auth.interface';
import { unwrap } from './api.helpers';
import { ApiResponse, ChatRequest, ChatResponse } from './api.models';

/** Hỏi đáp tri thức nhiều vòng — POST /chat, answer là Markdown, cites gắn số [^n]. */
@Injectable({ providedIn: 'root' })
export class ChatService {
  private readonly http = inject(HttpClient);
  private readonly base = inject(API_BASE_URL);

  /** Lịch sử gửi kèm tối đa ~10 lượt, mỗi content <= 4000 ký tự (binding BE). */
  chat(req: ChatRequest): Observable<ChatResponse> {
    return this.http.post<ApiResponse<ChatResponse>>(`${this.base}/chat`, req).pipe(unwrap());
  }
}
