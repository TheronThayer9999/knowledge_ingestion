import { inject, Injectable } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable } from 'rxjs';
import { API_BASE_URL } from '../auth/services/auth.interface';
import { unwrap } from './api.helpers';
import { ApiResponse, SearchRequest, SearchResponse } from './api.models';

/** Search tri thức (RAG chunks) — POST /search. */
@Injectable({ providedIn: 'root' })
export class SearchService {
  private readonly http = inject(HttpClient);
  private readonly base = inject(API_BASE_URL);

  search(req: SearchRequest): Observable<SearchResponse> {
    return this.http.post<ApiResponse<SearchResponse>>(`${this.base}/search`, req).pipe(unwrap());
  }
}
