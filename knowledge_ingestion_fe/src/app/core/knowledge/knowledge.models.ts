/**
 * Model miền "tri thức" của FE: danh mục (folder cây) + tài liệu + vé upload.
 * DTO khớp JSON backend (snake_case), model dùng camelCase cho UI —
 * mapper viết tay đặt cùng file như convention của auth.models.
 */

import type { TranslationKey } from '../i18n/lang.service';

export const DOC_TYPES = ['general', 'laws', 'table', 'paper', 'book', 'qa', 'manual'] as const;
export type DocType = (typeof DOC_TYPES)[number];

const DOC_TYPE_KEYS: Record<DocType, TranslationKey> = {
  general: 'DOC_TYPE_GENERAL',
  laws: 'DOC_TYPE_LAWS',
  table: 'DOC_TYPE_TABLE',
  paper: 'DOC_TYPE_PAPER',
  book: 'DOC_TYPE_BOOK',
  qa: 'DOC_TYPE_QA',
  manual: 'DOC_TYPE_MANUAL',
};

/** Key i18n của loại tài liệu — giá trị lạ rơi về "Chung" */
export function docTypeKey(docType: string): TranslationKey {
  return DOC_TYPE_KEYS[docType as DocType] ?? 'DOC_TYPE_GENERAL';
}

export interface Category {
  id: number;
  name: string;
  description: string;
  parentId: number | null;
  depth: number;
}

export interface Article {
  id: number;
  url: string;
  storageKey: string;
  name: string;
  contentType: string;
  docType: DocType | '';
  description: string;
  categoryId: number;
  /** Trạng thái pipeline (chỉ đọc): extract → index → compile */
  extractStatus: string;
  indexStatus: string;
  compileStatus: string;
  createdAt: string;
  updatedAt: string;
}

const STATUS_KEYS: Record<string, TranslationKey> = {
  pending: 'DOC_STATUS_PENDING',
  waiting: 'DOC_STATUS_PENDING',
  processing: 'DOC_STATUS_PROCESSING',
  running: 'DOC_STATUS_PROCESSING',
  done: 'DOC_STATUS_DONE',
  failed: 'DOC_STATUS_FAILED',
};

/** Key i18n của 1 trạng thái phase — giá trị lạ rơi về "Chờ xử lý" */
export function statusKey(status: string): TranslationKey {
  return STATUS_KEYS[status] ?? 'DOC_STATUS_PENDING';
}

/** Trạng thái gộp cả pipeline để hiện badge trên danh sách tài liệu */
export type ArticleStage = 'pending' | 'processing' | 'failed' | 'done';

/**
 * Gộp 3 phase thành 1 trạng thái: lỗi thắng, rồi đang chạy, rồi chờ; chỉ
 * khi cả 3 done mới coi là xong (UI ẩn badge). Giá trị lạ/rỗng coi như done
 * để bài cũ không bị gắn nhầm nhãn "chờ xử lý".
 */
export function articleStage(article: Article): ArticleStage {
  const phases = [article.extractStatus, article.indexStatus, article.compileStatus];
  if (phases.includes('failed')) {
    return 'failed';
  }
  if (phases.includes('processing') || phases.includes('running')) {
    return 'processing';
  }
  if (phases.includes('pending') || phases.includes('waiting')) {
    return 'pending';
  }
  return 'done';
}

export interface ArticleList {
  articles: Article[];
  total: number;
}

/** 1 mục trong cây heading của tài liệu (Chương > Mục > Điều) */
export interface SectionNode {
  name: string;
  from: number;
  summary: string;
  children: SectionNode[];
}

export interface ArticleTree {
  articleId: number;
  articleName: string;
  indexStatus: string;
  compileStatus: string;
  nodeCount: number;
  roots: SectionNode[];
}

/** Vé PUT thẳng lên kho — client gửi đúng method/contentType này */
export interface UploadTicket {
  uploadUrl: string;
  key: string;
  method: string;
  contentType: string;
  expiresAt: string;
}

export interface CreateCategoryPayload {
  name: string;
  description?: string;
  parentId?: number | null;
}

export interface CreateArticlePayload {
  name: string;
  categoryId: number;
  contentType: string;
  storageKey: string;
  docType?: DocType;
  description?: string;
}

/** Field bỏ trống = giữ nguyên; muốn bỏ cha thư mục thì dùng `moveToRoot` */
export interface UpdateCategoryPayload {
  name?: string;
  description?: string;
  parentId?: number;
  moveToRoot?: boolean;
}

/** Sửa từng phần tài liệu — nguồn (url/storage_key) là identity, không sửa được */
export interface UpdateArticlePayload {
  name?: string;
  categoryId?: number;
  docType?: DocType;
  description?: string;
}

// ---- DTO khớp JSON backend (snake_case) ----

export interface CategoryDto {
  id: number;
  user_id: number;
  name: string;
  description?: string;
  parent_id?: number | null;
  depth: number;
  created_at: string;
  updated_at: string;
}

export interface CategoryListDto {
  categories: CategoryDto[];
  total: number;
}

export interface ArticleDto {
  id: number;
  user_id: number;
  url?: string;
  storage_key?: string;
  name: string;
  content_type: string;
  doc_type?: string;
  description?: string;
  category_id: number;
  extract_status?: string;
  index_status?: string;
  compile_status?: string;
  created_at: string;
  updated_at: string;
}

export interface ArticleListDto {
  articles: ArticleDto[];
  total: number;
}

export interface SectionNodeDto {
  name: string;
  from: number;
  summary?: string;
  children?: SectionNodeDto[];
}

export interface ArticleTreeDto {
  article_id: number;
  article_name: string;
  index_status: string;
  compile_status: string;
  node_count: number;
  roots?: SectionNodeDto[];
}

export interface UploadTicketDto {
  upload_url: string;
  key: string;
  method: string;
  content_type: string;
  expires_at: string;
}

export function toCategory(dto: CategoryDto): Category {
  return {
    id: dto.id,
    name: dto.name,
    description: dto.description ?? '',
    parentId: dto.parent_id ?? null,
    depth: dto.depth ?? 0,
  };
}

export function toArticle(dto: ArticleDto): Article {
  return {
    id: dto.id,
    url: dto.url ?? '',
    storageKey: dto.storage_key ?? '',
    name: dto.name,
    contentType: dto.content_type,
    docType: (dto.doc_type ?? '') as DocType | '',
    description: dto.description ?? '',
    categoryId: dto.category_id,
    extractStatus: dto.extract_status ?? '',
    indexStatus: dto.index_status ?? '',
    compileStatus: dto.compile_status ?? '',
    createdAt: dto.created_at,
    updatedAt: dto.updated_at,
  };
}

export function toArticleTree(dto: ArticleTreeDto): ArticleTree {
  return {
    articleId: dto.article_id,
    articleName: dto.article_name,
    indexStatus: dto.index_status,
    compileStatus: dto.compile_status,
    nodeCount: dto.node_count,
    roots: (dto.roots ?? []).map(toSectionNode),
  };
}

function toSectionNode(dto: SectionNodeDto): SectionNode {
  return {
    name: dto.name,
    from: dto.from,
    summary: dto.summary ?? '',
    children: (dto.children ?? []).map(toSectionNode),
  };
}

export function toUploadTicket(dto: UploadTicketDto): UploadTicket {
  return {
    uploadUrl: dto.upload_url,
    key: dto.key,
    method: dto.method,
    contentType: dto.content_type,
    expiresAt: dto.expires_at,
  };
}

/** Node cây thư mục dựng từ danh sách phẳng (parent_id) */
export interface FolderNode {
  category: Category;
  children: FolderNode[];
}

/** Dựng cây thư mục; node mồ côi (cha đã xoá) được nâng lên root */
export function buildFolderTree(categories: Category[]): FolderNode[] {
  const byId = new Map<number, FolderNode>();
  for (const category of categories) {
    byId.set(category.id, { category, children: [] });
  }
  const roots: FolderNode[] = [];
  for (const node of byId.values()) {
    const parent = node.category.parentId === null ? undefined : byId.get(node.category.parentId);
    if (parent) {
      parent.children.push(node);
    } else {
      roots.push(node);
    }
  }
  sortNodes(roots);
  return roots;
}

function sortNodes(nodes: FolderNode[]): void {
  nodes.sort((a, b) => a.category.name.localeCompare(b.category.name, 'vi'));
  for (const node of nodes) {
    sortNodes(node.children);
  }
}

/** Mọi id trong nhánh (gồm chính nó) — lọc tài liệu của cả thư mục con */
export function subtreeIds(categories: Category[], rootId: number): Set<number> {
  const childrenOf = new Map<number, number[]>();
  for (const category of categories) {
    if (category.parentId === null) {
      continue;
    }
    const siblings = childrenOf.get(category.parentId) ?? [];
    siblings.push(category.id);
    childrenOf.set(category.parentId, siblings);
  }
  const result = new Set<number>();
  const stack = [rootId];
  while (stack.length > 0) {
    const id = stack.pop() as number;
    if (result.has(id)) {
      continue;
    }
    result.add(id);
    stack.push(...(childrenOf.get(id) ?? []));
  }
  return result;
}

/** 1 mục trong select thư mục (label đã thụt lề theo cấp) */
export interface CategoryOption {
  id: number;
  label: string;
}

/**
 * Danh sách thư mục phẳng theo đúng thứ tự cây, thụt lề theo cấp bằng NBSP.
 * `blocked` dùng khi sửa thư mục: loại chính nó + nhánh con khỏi lựa chọn cha.
 */
export function flattenCategories(
  categories: Category[],
  blocked?: Set<number>,
): CategoryOption[] {
  const visible = blocked
    ? categories.filter((category) => !blocked.has(category.id))
    : categories;
  const out: CategoryOption[] = [];
  const walk = (nodes: FolderNode[], level: number): void => {
    for (const node of nodes) {
      out.push({
        id: node.category.id,
        label: `${'\u00A0\u00A0\u00A0'.repeat(level)}${node.category.name}`,
      });
      walk(node.children, level + 1);
    }
  };
  walk(buildFolderTree(visible), 0);
  return out;
}
