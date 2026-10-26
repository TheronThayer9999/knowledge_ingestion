import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { beforeEach, describe, expect, it } from 'vitest';
import { environment } from '../../../environments/environment';
import {
  KnowledgeService,
  type UploadFileOutcome,
  type UploadFileRequest,
} from './knowledge.service';

/** URL khớp đúng những gì ApiService gửi: apiBaseUrl là tuyệt đối ở bản dev */
const api = (path: string): string => `${environment.apiBaseUrl}${path}`;

const ENVELOPE_OK = { status: true, code: 0, message: 'success' };

function pdf(name: string): File {
  return new File([new Uint8Array(8)], name, { type: 'application/pdf' });
}

function ticket(key: string) {
  return {
    upload_url: `https://s3.local/${key}`,
    key,
    method: 'PUT',
    content_type: 'application/pdf',
    expires_at: '2026-10-26T10:00:00Z',
  };
}

function articleDto(id: number, name: string) {
  return {
    id,
    name,
    content_type: 'application/pdf',
    category_id: 7,
    storage_key: `uploads/${name}.pdf`,
    doc_type: 'general',
    created_at: '2026-10-26T09:00:00Z',
    updated_at: '2026-10-26T09:00:00Z',
  };
}

const BATCH: UploadFileRequest[] = [
  { file: pdf('a.pdf'), name: 'A' },
  { file: pdf('b.pdf'), name: 'B' },
];

describe('KnowledgeService', () => {
  let service: KnowledgeService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    service = TestBed.inject(KnowledgeService);
    http = TestBed.inject(HttpTestingController);
  });

  it('presign 1 lần cho cả lô rồi PUT + tạo article từng file', async () => {
    const stages: string[] = [];
    const result = service
      .uploadDocuments(BATCH, { categoryId: 7, docType: 'general' }, (progress) =>
        stages.push(`${progress.index ?? 'all'}:${progress.stage}`),
      )
      .toPromise();

    const presign = http.expectOne(api('/uploads/presign/batch'));
    expect(presign.request.method).toBe('POST');
    expect((presign.request.body as FormData).getAll('files')).toHaveLength(2);
    presign.flush({
      ...ENVELOPE_OK,
      data: { items: [ticket('uploads/a.pdf'), ticket('uploads/b.pdf')] },
    });

    const putA = http.expectOne('https://s3.local/uploads/a.pdf');
    expect(putA.request.headers.get('Content-Type')).toBe('application/pdf');
    putA.flush('', { status: 200, statusText: 'OK' });

    const createA = http.expectOne(api('/articles'));
    expect(createA.request.body).toMatchObject({
      name: 'A',
      category_id: 7,
      storage_key: 'uploads/a.pdf',
      content_type: 'application/pdf',
    });
    createA.flush({ ...ENVELOPE_OK, data: articleDto(1, 'A') });

    http.expectOne('https://s3.local/uploads/b.pdf').flush('', { status: 200, statusText: 'OK' });
    http.expectOne(api('/articles')).flush({ ...ENVELOPE_OK, data: articleDto(2, 'B') });

    const outcomes = (await result) as UploadFileOutcome[];
    expect(outcomes.map((outcome) => outcome.article?.name)).toEqual(['A', 'B']);
    expect(outcomes.every((outcome) => outcome.error === null)).toBe(true);
    expect(stages).toEqual(['all:presign', '0:upload', '0:create', '1:upload', '1:create']);
    http.verify();
  });

  it('một file lỗi thì các file còn lại vẫn được đẩy tiếp', async () => {
    const result = service
      .uploadDocuments(BATCH, { categoryId: 7, docType: 'general' })
      .toPromise();

    http
      .expectOne(api('/uploads/presign/batch'))
      .flush({
        ...ENVELOPE_OK,
        data: { items: [ticket('uploads/a.pdf'), ticket('uploads/b.pdf')] },
      });
    http.expectOne('https://s3.local/uploads/a.pdf').flush('', { status: 200, statusText: 'OK' });
    http.expectOne(api('/articles')).flush({ ...ENVELOPE_OK, data: articleDto(1, 'A') });

    // Kho từ chối file thứ 2: lỗi nằm trong outcome, không làm hỏng cả lô
    http
      .expectOne('https://s3.local/uploads/b.pdf')
      .flush(
        { status: false, code: 500, message: 'kho từ chối' },
        { status: 500, statusText: 'Server Error' },
      );

    const outcomes = (await result) as UploadFileOutcome[];
    expect(outcomes[0].article?.id).toBe(1);
    expect(outcomes[1].article).toBeNull();
    expect(outcomes[1].error).toMatchObject({ status: 500 });
    http.verify();
  });
});
