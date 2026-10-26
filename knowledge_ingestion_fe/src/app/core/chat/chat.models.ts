/**
 * Model chat RAG: backend trả markdown kèm `cites` để FE render chip nguồn.
 * Lịch sử gửi lại đúng dạng role/content (user | assistant).
 * Phiên chat (Conversation/SavedMessage) là lớp trên: đã lưu ở DB, mở lại
 * được, và là nguồn lịch sử duy nhất khi gửi /chat.
 */

export type ChatRole = 'user' | 'assistant';

export interface ChatCite {
  n: number;
  articleId: number;
  pageNum: number;
  chunkIndex: number;
  snippet: string;
}

export interface ChatAnswer {
  answer: string;
  questionType: string;
  cites: ChatCite[];
  /** Đoạn model tự suy nghĩ trước khi đáp — rỗng khi backend không trả reasoning */
  reasoning: string;
}

export interface ChatTurn {
  role: ChatRole;
  content: string;
}

// ---- Phiên chat (Đợt 25): hội thoại lưu DB, là nguồn lịch sử duy nhất ----

/** Một phiên trong sidebar — `messageCount` do backend COUNT theo trang đang trả */
export interface Conversation {
  id: number;
  title: string;
  provider: string;
  model: string;
  thinking: boolean;
  /** Phạm vi đã chốt của phiên; rỗng = toàn bộ kho */
  categoryIds: number[];
  messageCount: number;
  lastMessageAt: Date;
  createdAt: Date;
}

/** Một lượt ĐÃ LƯU — khác ChatTurn (chưa lưu) ở chỗ có id, cites và phạm vi hiệu lực */
export interface SavedMessage {
  id: number;
  role: ChatRole;
  content: string;
  cites: ChatCite[];
  /** backend ghi scope thật của từng lượt, không suy từ phiên */
  categoryIds: number[];
  provider: string;
  model: string;
  thinking: boolean;
  createdAt: Date;
}

/** Payload mở phiên — mọi field tuỳ chọn, backend tự điền title ở lượt đầu */
export interface CreateConversationPayload {
  title?: string;
  categoryIds?: number[];
  thinking?: boolean;
}

// ---- DTO khớp JSON backend (snake_case) ----

export interface ChatCiteDto {
  n: number;
  article_id: number;
  page_num: number;
  chunk_index: number;
  snippet: string;
}

export interface ChatResponseDto {
  answer: string;
  question_type: string;
  cites?: ChatCiteDto[];
  reasoning?: string;
}

export interface ConversationDto {
  id: number;
  title: string;
  provider?: string;
  model?: string;
  thinking?: boolean;
  category_ids?: number[];
  message_count?: number;
  last_message_at: string;
  created_at: string;
}

export interface ChatMessageDto {
  id: number;
  role: string;
  content: string;
  cites?: ChatCiteDto[];
  effective_category_ids?: number[];
  provider?: string;
  model?: string;
  thinking?: boolean;
  created_at: string;
}

export function toCite(dto: ChatCiteDto): ChatCite {
  return {
    n: dto.n,
    articleId: dto.article_id,
    pageNum: dto.page_num,
    chunkIndex: dto.chunk_index,
    snippet: dto.snippet,
  };
}

export function toChatAnswer(dto: ChatResponseDto): ChatAnswer {
  return {
    answer: dto.answer,
    questionType: dto.question_type,
    reasoning: dto.reasoning ?? '',
    cites: (dto.cites ?? []).map(toCite),
  };
}

export function toConversation(dto: ConversationDto): Conversation {
  return {
    id: dto.id,
    title: dto.title ?? '',
    provider: dto.provider ?? '',
    model: dto.model ?? '',
    thinking: dto.thinking ?? false,
    categoryIds: dto.category_ids ?? [],
    messageCount: dto.message_count ?? 0,
    lastMessageAt: new Date(dto.last_message_at),
    createdAt: new Date(dto.created_at),
  };
}

/**
 * role là string ở DB nên phải chốt về union: value lạ rơi về 'assistant' để
 * bong bóng vẫn render được (kiểu bubble) thay vì vỡ template.
 */
export function toSavedMessage(dto: ChatMessageDto): SavedMessage {
  return {
    id: dto.id,
    role: dto.role === 'user' ? 'user' : 'assistant',
    content: dto.content,
    cites: (dto.cites ?? []).map(toCite),
    categoryIds: dto.effective_category_ids ?? [],
    provider: dto.provider ?? '',
    model: dto.model ?? '',
    thinking: dto.thinking ?? false,
    createdAt: new Date(dto.created_at),
  };
}

/** body snake_case cho POST /conversations — field rỗng KHÔNG gửi (omitempty ở BE) */
export function toCreateConversationBody(
  payload: CreateConversationPayload,
): Record<string, unknown> {
  const body: Record<string, unknown> = {};
  if (payload.title) {
    body['title'] = payload.title;
  }
  if (payload.categoryIds?.length) {
    body['category_ids'] = payload.categoryIds;
  }
  // Chỉ gửi khi bật: false là giá trị mặc định, gửi lên chỉ tổ đổi hình payload.
  if (payload.thinking) {
    body['thinking'] = true;
  }
  return body;
}
