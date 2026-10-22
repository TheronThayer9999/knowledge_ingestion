import { inject, Injectable } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';
import { Observable } from 'rxjs';
import { API_BASE_URL } from '../auth/services/auth.interface';
import { unwrap } from './api.helpers';
import {
  ApiResponse,
  Article,
  ArticleListResponse,
  ArticleSectionsResponse,
  ArticleTreeResponse,
  CreateArticleRequest,
  UpdateArticleRequest,
} from './api.models';

/** CRUD bài viết + PageIndex (sections) + cây mục (tree) của một bài. */
@Injectable({ providedIn: 'root' })
export class ArticlesService {
  private readonly http = inject(HttpClient);
  private readonly base = inject(API_BASE_URL);

  /** Lọc theo đúng 1 trong category_id/category_name (BE bắt), kèm phân trang. */
  list(params: { category_id?: number; category_name?: string; limit?: number; offset?: number }): Observable<ArticleListResponse> {
    let hp = new HttpParams();
    for (const [k, v] of Object.entries(params)) {
      if (v !== undefined && v !== null && v !== '') {
        hp = hp.set(k, String(v));
      }
    }
    return this.http.get<ApiResponse<ArticleListResponse>>(`${this.base}/articles`, { params: hp }).pipe(unwrap());
  }

  getById(id: number): Observable<Article> {
    return this.http.get<ApiResponse<Article>>(`${this.base}/articles/${id}`).pipe(unwrap());
  }

  create(req: CreateArticleRequest): Observable<Article> {
    return this.http.post<ApiResponse<Article>>(`${this.base}/articles`, req).pipe(unwrap());
  }

  update(id: number, req: UpdateArticleRequest): Observable<Article> {
    return this.http.put<ApiResponse<Article>>(`${this.base}/articles/${id}`, req).pipe(unwrap());
  }

  remove(id: number): Observable<Article> {
    return this.http.delete<ApiResponse<Article>>(`${this.base}/articles/${id}`).pipe(unwrap());
  }

  sections(id: number): Observable<ArticleSectionsResponse> {
    return this.http.get<ApiResponse<ArticleSectionsResponse>>(`${this.base}/articles/${id}/sections`).pipe(unwrap());
  }

  tree(id: number): Observable<ArticleTreeResponse> {
    return this.http.get<ApiResponse<ArticleTreeResponse>>(`${this.base}/articles/${id}/tree`).pipe(unwrap());
  }
}
