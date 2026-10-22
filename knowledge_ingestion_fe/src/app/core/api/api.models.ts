/**
 * Model wire (snake_case) khớp 1-1 contract backend Go (src/controller/dtos).
 * Đặt tên theo response Go; request chỉ khai khi FE gửi body riêng.
 */
import { User } from '../auth/models/auth.model';

/* ============================== Envelope ============================== */

export interface ApiResponse<T> {
  status: boolean;
  code: number;
  message: string;
  data: T | null;
}

/* ============================== Categories ============================== */

/** Node category phẳng — FE dựng cây bằng parent_id/depth (dtos.CategoryNodeResponse). */
export interface CategoryNode {
  id: number;
  user_id: number;
  name: string;
  description?: string;
  /** null/undefined = root. */
  parent_id?: number | null;
  /** 0 = root. */
  depth: number;
  created_at: string;
  updated_at: string;
}

export interface CategoryListResponse {
  categories: CategoryNode[];
  total: number;
}

export interface CategoryResponse {
  id: number;
  user_id: number;
  name: string;
  description?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateCategoryRequest {
  name: string;
  description?: string;
  /** nil = tạo root. */
  parent_id?: number;
}

export interface UpdateCategoryRequest {
  name?: string;
  description?: string;
  parent_id?: number;
  /** true = đưa về root; không gửi kèm parent_id. */
  move_to_root?: boolean;
}

/* ============================== Articles ============================== */

export interface Article {
  id: number;
  user_id: number;
  url?: string;
  storage_key?: string;
  name: string;
  content_type: string;
  doc_type?: string;
  description?: string;
  category_id: number;
  created_at: string;
  updated_at: string;
}

export interface ArticleListResponse {
  articles: Article[];
  total: number;
}

/** Đúng 1 nguồn: url HOẶC storage_key (sau khi presign upload xong). */
export interface CreateArticleRequest {
  url?: string;
  storage_key?: string;
  name: string;
  content_type: string;
  doc_type?: string;
  description?: string;
  category_id: number;
}

/** Partial update — field undefined = giữ nguyên. */
export interface UpdateArticleRequest {
  name?: string;
  content_type?: string;
  doc_type?: string;
  description?: string;
  category_id?: number;
}

/* -------- Sections / tree -------- */

export interface SectionSpan {
  name: string;
  from: number;
  summary: string;
}

export interface ArticleSectionsResponse {
  article_id: number;
  article_name: string;
  index_status: string;
  compile_status: string;
  section_count: number;
  sections: SectionSpan[];
}

export interface SectionTreeNode {
  name: string;
  from: number;
  summary: string;
  children: SectionTreeNode[];
}

export interface ArticleTreeResponse {
  article_id: number;
  article_name: string;
  index_status: string;
  compile_status: string;
  node_count: number;
  roots: SectionTreeNode[];
}

/* ============================== Search ============================== */

export interface SearchRequest {
  query: string;
  category_ids?: number[];
  limit?: number;
  score_threshold?: number;
  model?: string;
  question_type?: string;
}

export interface SearchHit {
  article_id: number;
  chunk_index: number;
  page_num: number;
  section: string;
  score: number;
  text: string;
}

export interface SearchResponse {
  hits: SearchHit[];
  total: number;
  question_type: string;
}

/* ============================== Chat ============================== */

export interface ChatHistoryItem {
  role: 'user' | 'assistant';
  content: string;
}

export interface ChatRequest {
  message: string;
  provider?: string;
  model?: string;
  category_ids?: number[];
  limit?: number;
  history?: ChatHistoryItem[];
}

export interface ChatCite {
  n: number;
  article_id: number;
  page_num: number;
  chunk_index: number;
  snippet: string;
}

export interface ChatResponse {
  answer: string;
  question_type: string;
  cites: ChatCite[];
}

/* ============================== Uploads (presign) ============================== */

export interface PresignUploadResponse {
  upload_url: string;
  key: string;
  method: string;
  /** BE đã chốt MIME và ký vào URL — PUT phải gửi đúng Content-Type này. */
  content_type: string;
  expires_at: string;
}

/* ============================== Users ============================== */

export interface UserListResponse {
  users: User[];
  total: number;
}

/* ============================== Account (auth phụ) ============================== */

export interface ChangePasswordRequest {
  old_password: string;
  new_password: string;
}

export interface LogoutResponse {
  revoked: number;
}

/* ============================== Admin ============================== */

export interface QueueArticleItem {
  id: number;
  name: string;
  storage_key: string;
  extract_status: string;
  index_status: string;
  attempts: number;
  next_retry_at: string;
  lease_expires_at: string;
  running_for_min: number;
  lease_expired: boolean;
  created_at: string;
  updated_at: string;
}

export interface QueuePhaseCounts {
  pending: number;
  processing: number;
  done: number;
  failed: number;
}

export interface QueuePhaseView {
  counts: QueuePhaseCounts;
  processing: QueueArticleItem[];
  failed: QueueArticleItem[];
  pending: QueueArticleItem[];
}

export interface PurgeItem {
  id: number;
  name: string;
  storage_key: string;
  deleted_at: string;
  waiting_min: number;
  undo_until: string;
  can_undo: boolean;
  extract_status: string;
  index_status: string;
}

export interface PurgeView {
  grace_period_min: number;
  batch_size: number;
  undoing: PurgeItem[];
  ready_to_purge: PurgeItem[];
}

export interface QueueStatusResponse {
  now: string;
  extract: QueuePhaseView;
  index: QueuePhaseView;
  purge: PurgeView;
}

export interface ConsistencyCheckResponse {
  orphan_closure_rows: number;
  missing_self_rows: number;
  cycle_indicators: number;
  orphan_articles: number;
  orphan_article_pages: number;
  healthy: boolean;
}

export interface EmbedCacheStatsResponse {
  count: number;
  bytes: number;
}

export interface EmbedCachePurgeRequest {
  older_than_days?: number;
}

export interface EmbedCachePurgeResponse {
  deleted: number;
}

/** Scope thao tác lô — đúng 1 trong article_ids hoặc category_id. */
export interface ResetScopeRequest {
  article_ids?: number[];
  category_id?: number;
}

/** Cùng hình dạng ResetScopeRequest (alias bên Go) — rebuild vector hàng loạt. */
export type RebuildVectorsRequest = ResetScopeRequest;

export interface ResetScopeResponse {
  reset_articles: number;
  article_ids: number[];
}
