import { inject, Injectable } from '@angular/core';
import { map, Observable } from 'rxjs';
import { ApiService } from '../api/api.service';
import { toChatAnswer, type ChatAnswer, type ChatResponseDto, type ChatTurn } from './chat.models';

/**
 * Gọi agent RAG của backend: POST /chat trả 1 lần (không streaming) —
 * history gửi kèm để nhiều vòng vẫn giữ ngữ cảnh, categoryIds để khoanh
 * phạm vi tài liệu, rỗng nghĩa là toàn bộ kho của user; thinking bật chế
 * độ suy luận sâu ở model (model không hỗ trợ thì coi như không).
 */
@Injectable({ providedIn: 'root' })
export class ChatService {
  private readonly api = inject(ApiService);

  send(
    message: string,
    history: ChatTurn[],
    categoryId: number | null,
    thinking = false,
  ): Observable<ChatAnswer> {
    const body: Record<string, unknown> = { message };
    if (history.length > 0) {
      body['history'] = history;
    }
    if (categoryId) {
      body['category_ids'] = [categoryId];
    }
    // Bật suy luận sâu: chỉ gửi khi bật để payload mặc định không đổi. Backend
    // chưa có field `thinking` thì ShouldBindJSON bỏ qua, không thành 400.
    if (thinking) {
      body['thinking'] = true;
    }
    return this.api.post<ChatResponseDto>('/chat', body).pipe(map(toChatAnswer));
  }
}
