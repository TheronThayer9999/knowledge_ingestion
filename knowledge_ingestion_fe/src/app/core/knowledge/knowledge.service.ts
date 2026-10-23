import { HttpClient, HttpHeaders } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { concatMap, forkJoin, map, Observable, of, switchMap, tap } from 'rxjs';
import { ApiService } from '../api/api.service';
import {
  toArticle,
  toArticleTree,
  toCategory,
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
  type UploadTicket,
  type UploadTicketDto,
} from './knowledge.models';

/** Bước upload để UI hiện đúng tiến trình: xin vé → PUT kho → tạo article */
export type UploadStage = 'presign' | 'upload' | 'create';

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

  getArticleTree(id: number): Observable<ArticleTree> {
    return this.api.get<ArticleTreeDto>(`/articles/${id}/tree`).pipe(map(toArticleTree));
  }

  deleteArticle(id: number): Observable<unknown> {
    return this.api.delete<unknown>(`/articles/${id}`);
  }

  /** Upload trọn luồng: presign → PUT thẳng kho → tạo article trỏ tới storage_key */
  uploadDocument(
    file: File,
    meta: { name: string; categoryId: number; docType: string; description?: string },
    onStage?: (stage: UploadStage) => void,
  ): Observable<Article> {
    onStage?.('presign');
    return this.presign(file).pipe(
      tap(() => onStage?.('upload')),
      concatMap((ticket) =>
        this.putObject(ticket, file).pipe(
          tap(() => onStage?.('create')),
          concatMap(() =>
            this.createArticle({
              name: meta.name,
              categoryId: meta.categoryId,
              contentType: ticket.contentType,
              storageKey: ticket.key,
              docType: meta.docType ? (meta.docType as DocType) : undefined,
              description: meta.description,
            }),
          ),
        ),
      ),
    );
  }

  /** Xin vé upload — multipart field tên `file` đúng như handler backend đọc */
  private presign(file: File): Observable<UploadTicket> {
    const form = new FormData();
    form.append('file', file, file.name);
    return this.api.post<UploadTicketDto>('/uploads/presign', form).pipe(map(toUploadTicket));
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
