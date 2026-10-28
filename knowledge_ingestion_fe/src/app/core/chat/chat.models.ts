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

/**
 * Biểu đồ backend DỰNG sẵn từ chunk đã dẫn (Đợt 26): mọi con số trong `spec`
 * đều do server cắt từ tài liệu, LLM chỉ được chọn nguồn `n` / kiểu / tiêu đề.
 * FE vì thế không tính toán gì thêm — chỉ đưa `spec` vào ECharts vẽ.
 */
export interface ChatChart {
  n: number;
  kind: string;
  /** ECharts option (bar/line: xAxis + series[0].data; pie: series[0].data) */
  spec: Record<string, unknown>;
  caption: string;
}

export interface ChatAnswer {
  answer: string;
  questionType: string;
  cites: ChatCite[];
  charts: ChatChart[];
  /** Đoạn model tự suy nghĩ trước khi đáp — rỗng khi backend không trả reasoning */
  reasoning: string;
  /**
   * Provider/model THỰC đã chạy (backend đã resolve default) — FE gắn badge
   * dưới câu trả lời. Rỗng khi backend cũ không trả field này.
   */
  provider: string;
  model: string;
}

export interface ChatTurn {
  role: ChatRole;
  content: string;
}

/**
 * Tệp đính kèm của MỘT lượt hỏi: backend trích text ngay lúc upload rồi chèn
 * vào ngữ cảnh của đúng lượt đó. Tệp KHÔNG nạp vào kho tri thức (không qua
 * worker/Qdrant) nên câu trả lời về tệp không có trích dẫn `[^n]`.
 */
export interface ChatAttachment {
  name: string;
  text: string;
  /** Số ký tự backend giữ lại sau trần cắt — 0 khi tệp không có chữ đọc được */
  chars: number;
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
  /** Biểu đồ đã lưu (cột `charts` jsonb) — mở lại phiên là vẽ y như lúc trả lời */
  charts: ChatChart[];
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
  /** Provider/model chốt của phiên; rỗng = theo cấu hình mặc định */
  provider?: string;
  model?: string;
}

// ---- DTO khớp JSON backend (snake_case) ----

export interface ChatCiteDto {
  n: number;
  article_id: number;
  page_num: number;
  chunk_index: number;
  snippet: string;
}

export interface ChatChartDto {
  n: number;
  kind: string;
  spec?: Record<string, unknown> | null;
  caption: string;
}

export interface ChatResponseDto {
  answer: string;
  question_type: string;
  cites?: ChatCiteDto[];
  charts?: ChatChartDto[];
  reasoning?: string;
  /** provider/model đã resolve — backend cũ không có thì coi như rỗng */
  provider?: string;
  model?: string;
}

export interface ChatAttachmentDto {
  name: string;
  text: string;
  chars?: number;
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
  charts?: ChatChartDto[];
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

export function toChatChart(dto: ChatChartDto): ChatChart {
  return {
    n: dto.n,
    kind: dto.kind,
    // spec thiếu thì để object rỗng: component vẽ chỗ trống chứ không vỡ template
    spec: dto.spec ?? {},
    caption: dto.caption ?? '',
  };
}

/** Tệp đã trích: `chars` là số ký tự backend giữ (thiếu ở BE cũ → coi như 0) */
export function toChatAttachment(dto: ChatAttachmentDto): ChatAttachment {
  return {
    name: dto.name,
    text: dto.text,
    chars: dto.chars ?? 0,
  };
}

export function toChatAnswer(dto: ChatResponseDto): ChatAnswer {
  return {
    answer: dto.answer,
    questionType: dto.question_type,
    reasoning: dto.reasoning ?? '',
    cites: (dto.cites ?? []).map(toCite),
    charts: (dto.charts ?? []).map(toChatChart),
    provider: dto.provider ?? '',
    model: dto.model ?? '',
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
    charts: (dto.charts ?? []).map(toChatChart),
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
  // Provider/model: rỗng = theo cấu hình, nên KHÔNG gửi key rỗng (backend đọc
  // "trống" là lấy mặc định — gửi chuỗi rỗng sẽ ghi đè lựa chọn của phiên).
  if (payload.provider) {
    body['provider'] = payload.provider;
  }
  if (payload.model) {
    body['model'] = payload.model;
  }
  return body;
}

/** Trần độ dài tiêu đề phiên — sidebar hẹp, dài hơn nữa thì chỉ tổ ellipsis */
const SESSION_TITLE_MAX_RUNES = 40;

/**
 * Tiêu đề tự sinh từ câu hỏi đầu (Đợt 27 nhóm 1): ~40 ký tự, cắt ở RANH GIỚI
 * TỪ chứ không xé giữa chữ, đếm theo rune để tiếng Việt có dấu không bị cắt ở
 * nửa âm tiết. Backend cũng có ClipTitle (cắt cứng) nhưng SetTitleIfEmpty chỉ
 * điền khi title còn rỗng — FE gửi title trước thì câu chữ trên sidebar là FE
 * quyết, thấy ngay lúc bấm gửi thay vì chờ nạp lại sidebar.
 */
export function clipSessionTitle(question: string): string {
  const text = question.trim().replace(/\s+/g, ' ');
  const characters = Array.from(text);
  if (characters.length <= SESSION_TITLE_MAX_RUNES) {
    return text;
  }
  // +1 rune để nhìn thấy khoảng trắng kế tiếp: có dấu cách trong vùng thì cắt ở đó
  const head = characters.slice(0, SESSION_TITLE_MAX_RUNES + 1).join('');
  const boundary = head.lastIndexOf(' ');
  const taken =
    boundary > 0 ? characters.slice(0, boundary) : characters.slice(0, SESSION_TITLE_MAX_RUNES);
  // Dấu câu treo cuối ("Quy định về phụ cấp,") nhìn như câu bị cắt đôi
  return `${taken.join('').replace(/[\s,;:.\-!?]+$/, '')}…`;
}
