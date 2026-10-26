import { HttpClient, HttpHeaders } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import {
  catchError,
  concatMap,
  forkJoin,
  from,
  map,
  Observable,
  of,
  switchMap,
  tap,
  toArray,
} from 'rxjs';
import { ApiService } from '../api/api.service';
import {
  toArticle,
  toArticleTree,
  toCategory,
  toPipelineStatus,
  toUploadTicket,
  type Article,
  type ArticleList,
  type ArticleListDto,
  type ArticleTree,
  type ArticleTreeDto,
  type ArticleDto,
  type Category,
  type CategoryDto,
  type CategoryListDto,
  type CreateArticlePayload,
  type CreateCategoryPayload,
  type DocType,
  type PipelineStatus,
  type PipelineStatusDto,
  type UpdateArticlePayload,
  type UpdateCategoryPayload,
  type UploadTicket,
  type UploadTicketDto,
} from './knowledge.models';

/** Bước upload để UI hiện đúng tiến trình: xin vé → PUT kho → tạo article */
export type UploadStage = 'presign' | 'upload' | 'create';

/** Một file trong lô — `name` là tên tài liệu người dùng đã sửa (mặc định bỏ đuôi file) */
export interface UploadFileRequest {
  file: File;
  name: string;
}

/** Kết quả từng file: hoặc article, hoặc lỗi — một file hỏng không giết cả lô */
export interface UploadFileOutcome {
  file: File;
  article: Article | null;
  error: unknown;
}

/**
 * Tiến trình upload: `index === null` là bước của cả lô (presign ký 1 request cho
 * mọi file), có index là bước của riêng file đó trong lô gửi lên.
 */
export interface UploadProgress {
  index: number | null;
  stage: UploadStage;
}

/** Thông tin chung của cả lô — mọi file dùng chung thư mục/loại/mô tả */
export interface UploadBatchMeta {
  categoryId: number;
  docType: string;
  description?: string;
}

/** Trần phân trang của backend (`utils.MaxListLimit`) */
const PAGE_SIZE = 100;

/**
 * Miền tri thức: cây danh mục + tài liệu + upload 3 bước.
 * CRUD thường đi qua ApiService; riêng PUT lên kho là request thô (không có
 * envelope) nên dùng HttpClient trực tiếp — đây là ngoại lệ duy nhất.
 */
@Injectable({ providedIn: 'root' })
export class KnowledgeService {
  private readonly api = inject(ApiService);
  private readonly http = inject(HttpClient);

  listCategories(): Observable<Category[]> {
    return this.api
      .get<CategoryListDto>('/categories')
      .pipe(map((res) => (res.categories ?? []).map(toCategory)));
  }

  createCategory(payload: CreateCategoryPayload): Observable<Category> {
    const body: Record<string, unknown> = { name: payload.name };
    if (payload.description) {
      body['description'] = payload.description;
    }
    if (payload.parentId) {
      body['parent_id'] = payload.parentId;
    }
    return this.api.post<CategoryDto>('/categories', body).pipe(map(toCategory));
  }

  deleteCategory(id: number): Observable<unknown> {
    return this.api.delete<unknown>(`/categories/${id}`);
  }

  /**
   * Sửa thư mục từng phần. Backend không nhận `parent_id: null` để "bỏ cha"
   * nên đổi về gốc phải gửi cờ `move_to_root` (gửi kèm parent_id là 400).
   */
  updateCategory(id: number, payload: UpdateCategoryPayload): Observable<Category> {
    const body: Record<string, unknown> = {};
    if (payload.name !== undefined) {
      body['name'] = payload.name;
    }
    if (payload.description !== undefined) {
      body['description'] = payload.description;
    }
    if (payload.moveToRoot) {
      body['move_to_root'] = true;
    } else if (payload.parentId) {
      body['parent_id'] = payload.parentId;
    }
    return this.api.put<CategoryDto>(`/categories/${id}`, body).pipe(map(toCategory));
  }

  /** Backend bắt buộc lọc đúng 1 danh mục — muốn xem tất cả thì dùng listAllArticles */
  listArticles(categoryId: number, limit = PAGE_SIZE, offset = 0): Observable<ArticleList> {
    return this.api
      .get<ArticleListDto>('/articles', { category_id: categoryId, limit, offset })
      .pipe(
        map((res) => ({
          articles: (res.articles ?? []).map(toArticle),
          total: res.total ?? 0,
        })),
      );
  }

  /**
   * Gộp tài liệu của mọi danh mục (backend không có endpoint "list all").
   * Mỗi danh mục đọc lần lượt từng trang 100 cho tới khi hết.
   */
  listAllArticles(): Observable<Article[]> {
    return this.listCategories().pipe(
      switchMap((categories) =>
        categories.length === 0
          ? of<Article[]>([])
          : forkJoin(categories.map((category) => this.listCategoryPages(category.id))),
      ),
      map((pages) => pages.flat()),
    );
  }

  /**
   * Trạng thái pipeline (id + 3 phase) và nhịp sống worker trong 1 request —
   * UI dùng để tự cập nhật badge tiến trình và cảnh báo khi worker đã tắt.
   */
  getPipelineStatus(): Observable<PipelineStatus> {
    return this.api.get<PipelineStatusDto>('/pipeline/articles').pipe(map(toPipelineStatus));
  }

  getArticleTree(id: number): Observable<ArticleTree> {
    return this.api.get<ArticleTreeDto>(`/articles/${id}/tree`).pipe(map(toArticleTree));
  }

  /** Một tài liệu theo id (404 khi không tồn tại hoặc không thuộc owner) — chat dùng khi bấm nguồn trích dẫn */
  getArticle(id: number): Observable<Article> {
    return this.api.get<ArticleDto>(`/articles/${id}`).pipe(map(toArticle));
  }

  deleteArticle(id: number): Observable<unknown> {
    return this.api.delete<unknown>(`/articles/${id}`);
  }

  /** Sửa tên/mô tả/loại và chuyển thư mục — nguồn file giữ nguyên */
  updateArticle(id: number, payload: UpdateArticlePayload): Observable<Article> {
    const body: Record<string, unknown> = {};
    if (payload.name !== undefined) {
      body['name'] = payload.name;
    }
    if (payload.categoryId !== undefined) {
      body['category_id'] = payload.categoryId;
    }
    if (payload.docType !== undefined) {
      body['doc_type'] = payload.docType;
    }
    if (payload.description !== undefined) {
      body['description'] = payload.description;
    }
    return this.api.put<ArticleDto>(`/articles/${id}`, body).pipe(map(toArticle));
  }

  /**
   * Upload nhiều file trong một lượt: 1 request presign cho cả lô rồi PUT + tạo
   * article lần lượt từng file. File nào lỗi thì ghi lỗi vào outcome của file đó
   * và lô vẫn chạy tiếp; presign lỗi là cả lô lỗi (backend validate trọn lô trước
   * khi ký nên không có chuyện ký nửa chừng).
   */
  uploadDocuments(
    files: UploadFileRequest[],
    meta: UploadBatchMeta,
    onProgress?: (progress: UploadProgress) => void,
  ): Observable<UploadFileOutcome[]> {
    onProgress?.({ index: null, stage: 'presign' });
    return this.presignMany(files.map((item) => item.file)).pipe(
      concatMap((tickets) =>
        from(tickets.map((ticket, index) => ({ ticket, index }))).pipe(
          concatMap(({ ticket, index }) =>
            this.uploadOne(files[index], meta, ticket, index, onProgress),
          ),
          toArray(),
        ),
      ),
    );
  }

  /** Một file: PUT lên kho rồi tạo article trỏ tới storage_key */
  private uploadOne(
    item: UploadFileRequest,
    meta: UploadBatchMeta,
    ticket: UploadTicket,
    index: number,
    onProgress?: (progress: UploadProgress) => void,
  ): Observable<UploadFileOutcome> {
    onProgress?.({ index, stage: 'upload' });
    return this.putObject(ticket, item.file).pipe(
      tap(() => onProgress?.({ index, stage: 'create' })),
      concatMap(() =>
        this.createArticle({
          name: item.name,
          categoryId: meta.categoryId,
          contentType: ticket.contentType,
          storageKey: ticket.key,
          docType: meta.docType ? (meta.docType as DocType) : undefined,
          description: meta.description,
        }),
      ),
      map((article) => ({ file: item.file, article, error: null })),
      catchError((error: unknown) => of({ file: item.file, article: null, error })),
    );
  }

  /** Xin vé cho cả lô — multipart field tên `files` đúng như handler batch đọc */
  private presignMany(files: File[]): Observable<UploadTicket[]> {
    const form = new FormData();
    for (const file of files) {
      form.append('files', file, file.name);
    }
    return this.api
      .post<{ items: UploadTicketDto[] }>('/uploads/presign/batch', form)
      .pipe(map((res) => (res.items ?? []).map(toUploadTicket)));
  }

  /** PUT thô lên S3-compatible gateway — Content-Type phải khớp chữ ký của vé */
  private putObject(ticket: UploadTicket, file: File): Observable<unknown> {
    return this.http.put(ticket.uploadUrl, file, {
      headers: new HttpHeaders({ 'Content-Type': ticket.contentType }),
      responseType: 'text',
    });
  }

  private listCategoryPages(categoryId: number, offset = 0): Observable<Article[]> {
    return this.listArticles(categoryId, PAGE_SIZE, offset).pipe(
      concatMap((page) =>
        page.articles.length < PAGE_SIZE
          ? of(page.articles)
          : this.listCategoryPages(categoryId, offset + PAGE_SIZE).pipe(
              map((next) => [...page.articles, ...next]),
            ),
      ),
    );
  }

  private createArticle(payload: CreateArticlePayload): Observable<Article> {
    const body: Record<string, unknown> = {
      name: payload.name,
      category_id: payload.categoryId,
      content_type: payload.contentType,
      storage_key: payload.storageKey,
    };
    if (payload.docType) {
      body['doc_type'] = payload.docType;
    }
    if (payload.description) {
      body['description'] = payload.description;
    }
    return this.api.post<ArticleDto>('/articles', body).pipe(map(toArticle));
  }
}
