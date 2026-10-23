/**
 * Model chat RAG: backend trả markdown kèm `cites` để FE render chip nguồn.
 * Lịch sử gửi lại đúng dạng role/content (user | assistant).
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
}

export interface ChatTurn {
  role: ChatRole;
  content: string;
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
}

export function toChatAnswer(dto: ChatResponseDto): ChatAnswer {
  return {
    answer: dto.answer,
    questionType: dto.question_type,
    cites: (dto.cites ?? []).map((cite) => ({
      n: cite.n,
      articleId: cite.article_id,
      pageNum: cite.page_num,
      chunkIndex: cite.chunk_index,
      snippet: cite.snippet,
    })),
  };
}
