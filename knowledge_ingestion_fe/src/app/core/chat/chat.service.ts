import { inject, Injectable } from '@angular/core';
import { map, Observable } from 'rxjs';
import { ApiService } from '../api/api.service';
import { toChatAnswer, type ChatAnswer, type ChatResponseDto, type ChatTurn } from './chat.models';

/** Một lượt gửi đi — `conversationId` là con trỏ quyết định cách tính lịch sử */
export interface ChatSendRequest {
  message: string;
  /** > 0: server nạp lịch sử từ DB phiên này, `history` bị bỏ qua (một nguồn thật) */
  conversationId?: number | null;
  /** Đường legacy khi không có phiên: client kể lại transcript. FE không dùng nhánh này nữa */
  history?: ChatTurn[];
  /** Phạm vi của lượt; rỗng = backend lấy theo phạm vi đã chốt của phiên */
  categoryId?: number | null;
  thinking?: boolean;
  /**
   * Provider/model của lượt (Đợt 28). Rỗng = backend dùng mặc định cấu hình,
   * nên KHÔNG gửi chuỗi rỗng — gửi "" sẽ ghi đè lựa chọn đã lưu của phiên.
   */
  provider?: string;
  model?: string;
}

/**
 * Gọi agent RAG của backend: POST /chat trả 1 lần (không streaming) —
 * categoryIds để khoanh phạm vi tài liệu (rỗng = toàn kho của user); thinking
 * bật chế độ suy luận sâu ở model (model không hỗ trợ thì coi như không).
 *
 * Có `conversationId` thì TUYỆT ĐỐI không gửi `history`: backend coi lịch sử
 * trong DB của phiên là nguồn duy nhất, gửi cả hai là tạo hai sự thật cùng lúc.
 */
@Injectable({ providedIn: 'root' })
export class ChatService {
  private readonly api = inject(ApiService);

  send(request: ChatSendRequest): Observable<ChatAnswer> {
    const conversationId = request.conversationId ?? 0;
    const body: Record<string, unknown> = { message: request.message };
    if (conversationId > 0) {
      body['conversation_id'] = conversationId;
    } else if (request.history?.length) {
      body['history'] = request.history;
    }
    if (request.categoryId) {
      body['category_ids'] = [request.categoryId];
    }
    // Bật suy luận sâu: chỉ gửi khi bật để payload mặc định không đổi. Backend
    // đưa request sang bản agent đã bật `think` của model; provider nào không
    // có công tắc đó thì cờ tự vô tác dụng, chat vẫn chạy.
    if (request.thinking) {
      body['thinking'] = true;
    }
    // Provider/model chỉ gửi khi có: để trống thì backend lấy mặc định cấu hình
    // (hoặc lựa chọn đã lưu của phiên).
    if (request.provider) {
      body['provider'] = request.provider;
    }
    if (request.model) {
      body['model'] = request.model;
    }
    return this.api.post<ChatResponseDto>('/chat', body).pipe(map(toChatAnswer));
  }
}
