import { inject, Injectable } from '@angular/core';
import { map, Observable } from 'rxjs';
import { ApiService } from '../api/api.service';
import {
  toConversation,
  toCreateConversationBody,
  toSavedMessage,
  type ChatMessageDto,
  type Conversation,
  type ConversationDto,
  type CreateConversationPayload,
  type SavedMessage,
} from './chat.models';

/** Trần phân trang của backend: limit tối đa 100 phiên / 200 message */
export const CONVERSATION_PAGE = 20;
export const MESSAGE_PAGE = 50;

/**
 * CRUD phiên chat + lịch sử message (6 route `/conversations*`).
 *
 * Không có tham số user_id ở bất kỳ đâu: backend lấy owner từ token, phiên của
 * người khác trả 404 (chứ không phải 403) nên UI không cần nhánh "cấm xem".
 * Service trả domain model — DTO snake_case dừng lại ở file này.
 */
@Injectable({ providedIn: 'root' })
export class ConversationService {
  private readonly api = inject(ApiService);

  /** newest-first theo lượt cuối; `before` là cursor keyset (id phiên cuối trang trước) */
  list(before?: number, limit = CONVERSATION_PAGE): Observable<Conversation[]> {
    return this.api
      .get<ConversationDto[]>('/conversations', pageQuery(limit, before))
      .pipe(map((rows) => (rows ?? []).map(toConversation)));
  }

  create(payload: CreateConversationPayload): Observable<Conversation> {
    return this.api
      .post<ConversationDto>('/conversations', toCreateConversationBody(payload))
      .pipe(map(toConversation));
  }

  rename(id: number, title: string): Observable<Conversation> {
    return this.api
      .patch<ConversationDto>(`/conversations/${id}`, { title })
      .pipe(map(toConversation));
  }

  /** Xoá mềm: message vẫn nằm ở DB, nên UI không được nói "đã xoá vĩnh viễn" */
  remove(id: number): Observable<Conversation> {
    return this.api.delete<ConversationDto>(`/conversations/${id}`).pipe(map(toConversation));
  }

  /**
   * Lịch sử một phiên, phục vụ mở lại. Backend đã lật kết quả thành oldest-first
   * trong cửa sổ nên FE append thẳng vào DOM theo thứ tự đọc.
   */
  messages(id: number, before?: number, limit = MESSAGE_PAGE): Observable<SavedMessage[]> {
    return this.api
      .get<ChatMessageDto[]>(`/conversations/${id}/messages`, pageQuery(limit, before))
      .pipe(map((rows) => (rows ?? []).map(toSavedMessage)));
  }
}

/** `before=0` là hợp lệ về kiểu nhưng vô nghĩa với backend (binding gt=0) — bỏ hẳn */
function pageQuery(limit: number, before?: number): Record<string, number> {
  return before && before > 0 ? { limit, before } : { limit };
}
