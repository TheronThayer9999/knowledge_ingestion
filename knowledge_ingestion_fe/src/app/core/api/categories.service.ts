import { inject, Injectable } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable } from 'rxjs';
import { API_BASE_URL } from '../auth/services/auth.interface';
import { unwrap } from './api.helpers';
import {
  ApiResponse,
  CategoryResponse,
  CategoryListResponse,
  CreateCategoryRequest,
  UpdateCategoryRequest,
} from './api.models';

/** CRUD danh mục — GET trả danh sách PHẲNG kèm parent_id/depth, FE tự hiển thị cây. */
@Injectable({ providedIn: 'root' })
export class CategoriesService {
  private readonly http = inject(HttpClient);
  private readonly base = inject(API_BASE_URL);

  list(): Observable<CategoryListResponse> {
    return this.http.get<ApiResponse<CategoryListResponse>>(`${this.base}/categories`).pipe(unwrap());
  }

  getById(id: number): Observable<CategoryResponse> {
    return this.http.get<ApiResponse<CategoryResponse>>(`${this.base}/categories/${id}`).pipe(unwrap());
  }

  create(req: CreateCategoryRequest): Observable<CategoryResponse> {
    return this.http.post<ApiResponse<CategoryResponse>>(`${this.base}/categories`, req).pipe(unwrap());
  }

  update(id: number, req: UpdateCategoryRequest): Observable<CategoryResponse> {
    return this.http.put<ApiResponse<CategoryResponse>>(`${this.base}/categories/${id}`, req).pipe(unwrap());
  }

  remove(id: number): Observable<CategoryResponse> {
    return this.http.delete<ApiResponse<CategoryResponse>>(`${this.base}/categories/${id}`).pipe(unwrap());
  }
}
