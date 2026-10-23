import { inject, Injectable } from '@angular/core';
import { map, Observable } from 'rxjs';
import { ApiService } from '../api/api.service';
import { toChatAnswer, type ChatAnswer, type ChatResponseDto, type ChatTurn } from './chat.models';

/**
 * Gọi agent RAG của backend: POST /chat trả 1 lần (không streaming) —
 * history gửi kèm để nhiều vòng vẫn giữ ngữ cảnh, categoryIds để khoanh
 * phạm vi tài liệu, rỗng nghĩa là toàn bộ kho của user.
 */
@Injectable({ providedIn: 'root' })
export class ChatService {
  private readonly api = inject(ApiService);

  send(message: string, history: ChatTurn[], categoryId: number | null): Observable<ChatAnswer> {
    const body: Record<string, unknown> = { message };
    if (history.length > 0) {
      body['history'] = history;
    }
    if (categoryId) {
      body['category_ids'] = [categoryId];
    }
    return this.api.post<ChatResponseDto>('/chat', body).pipe(map(toChatAnswer));
  }
}
