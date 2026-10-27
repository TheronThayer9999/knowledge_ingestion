import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { beforeEach, describe, expect, it } from 'vitest';
import { environment } from '../../../environments/environment';
import { CONVERSATION_PAGE, ConversationService } from './conversation.service';

/** URL khớp đúng những gì ApiService gửi: apiBaseUrl là tuyệt đối ở bản dev */
const api = (path: string): string => `${environment.apiBaseUrl}${path}`;

const ENVELOPE_OK = { status: true, code: 0, message: 'success' };

function conversationDto(id: number, title: string) {
  return {
    id,
    title,
    provider: 'openrouter',
    model: 'google/gemma-4-31b-it:free',
    thinking: true,
    category_ids: [7],
    message_count: 4,
    last_message_at: '2026-10-26T09:30:00Z',
    created_at: '2026-10-25T09:00:00Z',
  };
}

describe('ConversationService', () => {
  let service: ConversationService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    service = TestBed.inject(ConversationService);
    http = TestBed.inject(HttpTestingController);
  });

  it('list: GET /conversations với limit, map snake_case + Date', async () => {
    const result = service.list().toPromise();

    const req = http.expectOne(
      (r) => r.url === api('/conversations') && r.params.get('limit') === String(CONVERSATION_PAGE),
    );
    expect(req.request.method).toBe('GET');
    req.flush({ ...ENVELOPE_OK, data: [conversationDto(12, 'Phụ cấp công tác')] });

    const rows = (await result)!;
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({
      id: 12,
      title: 'Phụ cấp công tác',
      provider: 'openrouter',
      thinking: true,
      categoryIds: [7],
      messageCount: 4,
    });
    expect(rows[0].lastMessageAt.getTime()).toBe(Date.parse('2026-10-26T09:30:00Z'));
    http.verify();
  });

  it('list(before): cursor keyset đi kèm, không có before thì không gửi', async () => {
    const first = service.list().toPromise();
    http
      .expectOne((r) => r.url === api('/conversations') && !r.params.has('before'))
      .flush({ ...ENVELOPE_OK, data: [] });
    await first;

    const second = service.list(41).toPromise();
    http
      .expectOne((r) => r.url === api('/conversations') && r.params.get('before') === '41')
      .flush({ ...ENVELOPE_OK, data: [] });
    await second;
    http.verify();
  });

  it('create: field rỗng không nằm trong body (BE omitempty)', async () => {
    const result = service.create({ categoryIds: [], thinking: false }).toPromise();

    const req = http.expectOne(api('/conversations'));
    expect(req.request.method).toBe('POST');
    expect(req.request.body).toEqual({});
    req.flush({ ...ENVELOPE_OK, data: conversationDto(13, '') });

    expect((await result)!.id).toBe(13);
    http.verify();
  });

  it('rename: PATCH /conversations/:id chỉ gửi title', async () => {
    const result = service.rename(12, 'Quy chế phí').toPromise();

    const req = http.expectOne(api('/conversations/12'));
    expect(req.request.method).toBe('PATCH');
    expect(req.request.body).toEqual({ title: 'Quy chế phí' });
    req.flush({ ...ENVELOPE_OK, data: conversationDto(12, 'Quy chế phí') });

    expect((await result)!.title).toBe('Quy chế phí');
    http.verify();
  });

  it('remove: DELETE /conversations/:id', async () => {
    const result = service.remove(12).toPromise();

    const req = http.expectOne(api('/conversations/12'));
    expect(req.request.method).toBe('DELETE');
    req.flush({ ...ENVELOPE_OK, data: conversationDto(12, 'x') });

    expect((await result)!.id).toBe(12);
    http.verify();
  });

  it('messages: giữ nguyên thứ tự oldest-first và map cites/scope', async () => {
    const result = service.messages(12).toPromise();

    // expectOne(string) so với URL đã nối query — ở đây cần so riêng path
    const req = http.expectOne((r) => r.url === api('/conversations/12/messages'));
    expect(req.request.method).toBe('GET');
    expect(req.request.params.get('limit')).toBe('50');
    req.flush({
      ...ENVELOPE_OK,
      data: [
        {
          id: 107,
          role: 'user',
          content: 'Phụ cấp là bao nhiêu?',
          cites: null,
          effective_category_ids: [7],
          created_at: '2026-10-26T09:00:00Z',
        },
        {
          id: 108,
          role: 'assistant',
          content: 'Mức phí là 8.000 VNĐ[^1].',
          cites: [{ n: 1, article_id: 8, page_num: 4, chunk_index: 2, snippet: 'Điều 13' }],
          charts: [
            {
              n: 1,
              kind: 'bar',
              spec: { series: [{ type: 'bar', data: [800000] }] },
              caption: 'Nguồn: [^1] · Phụ cấp theo nhân viên',
            },
          ],
          provider: 'openrouter',
          thinking: false,
          created_at: '2026-10-26T09:00:30Z',
        },
      ],
    });

    const rows = (await result)!;
    expect(rows.map((row) => row.id)).toEqual([107, 108]);
    expect(rows[0].cites).toEqual([]);
    expect(rows[0].charts).toEqual([]);
    expect(rows[0].categoryIds).toEqual([7]);
    expect(rows[1].cites).toEqual([
      { n: 1, articleId: 8, pageNum: 4, chunkIndex: 2, snippet: 'Điều 13' },
    ]);
    // Mở lại phiên thì biểu đồ phải hiện ra y hệt lúc trả lời (Đợt 26)
    expect(rows[1].charts).toEqual([
      {
        n: 1,
        kind: 'bar',
        spec: { series: [{ type: 'bar', data: [800000] }] },
        caption: 'Nguồn: [^1] · Phụ cấp theo nhân viên',
      },
    ]);
    expect(rows[1].thinking).toBe(false);
    http.verify();
  });

  it('messages: role lạ vẫn đọc được (rơi về assistant thay vì vỡ template)', async () => {
    const result = service.messages(12).toPromise();
    http
      .expectOne((r) => r.url === api('/conversations/12/messages'))
      .flush({
        ...ENVELOPE_OK,
        data: [{ id: 1, role: 'system', content: '?', created_at: '2026-10-26T09:00:00Z' }],
      });

    expect((await result)![0].role).toBe('assistant');
    http.verify();
  });
});
