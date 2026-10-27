import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { beforeEach, describe, expect, it } from 'vitest';
import { environment } from '../../../environments/environment';
import { ChatService } from './chat.service';

const api = (path: string): string => `${environment.apiBaseUrl}${path}`;

const ENVELOPE_OK = { status: true, code: 0, message: 'success' };

const ANSWER = {
  answer: 'Mức phí là 8.000 VNĐ[^1].',
  question_type: 'lookup',
  cites: [{ n: 1, article_id: 8, page_num: 4, chunk_index: 2, snippet: 'Điều 13' }],
};

describe('ChatService', () => {
  let service: ChatService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    service = TestBed.inject(ChatService);
    http = TestBed.inject(HttpTestingController);
  });

  /**
   * Nghiệm thu S1 ở tầng payload: đã gắn phiên thì KHÔNG được gửi history —
   * server nạp lịch sử từ DB, hai nguồn cùng lúc là hai sự thật.
   */
  it('có conversationId: gửi id, tuyệt đối không gửi history', async () => {
    const result = service
      .send({
        message: 'Vậy bản cũ thì sao?',
        conversationId: 12,
        history: [{ role: 'user', content: 'câu bị bỏ qua' }],
      })
      .toPromise();

    const req = http.expectOne(api('/chat'));
    expect(req.request.body).toEqual({ message: 'Vậy bản cũ thì sao?', conversation_id: 12 });
    req.flush({ ...ENVELOPE_OK, data: ANSWER });

    expect((await result)!.cites).toHaveLength(1);
    http.verify();
  });

  it('không có phiên (đường legacy): history vẫn đi, không có conversation_id', async () => {
    const result = service
      .send({ message: 'hỏi', history: [{ role: 'assistant', content: 'đáp' }] })
      .toPromise();

    const req = http.expectOne(api('/chat'));
    expect(req.request.body).toEqual({
      message: 'hỏi',
      history: [{ role: 'assistant', content: 'đáp' }],
    });
    req.flush({ ...ENVELOPE_OK, data: ANSWER });
    await result;
    http.verify();
  });

  it('gửi 0id/0 phạm vi thì payload tối giản như mặc định cũ', async () => {
    const result = service.send({ message: 'hỏi', conversationId: 0, categoryId: 0 }).toPromise();

    const req = http.expectOne(api('/chat'));
    expect(req.request.body).toEqual({ message: 'hỏi' });
    req.flush({ ...ENVELOPE_OK, data: ANSWER });
    await result;
    http.verify();
  });

  it('phạm vi + thinking: category_ids là mảng, thinking chỉ gửi khi bật', async () => {
    const result = service.send({ message: 'hỏi', categoryId: 7, thinking: true }).toPromise();

    const req = http.expectOne(api('/chat'));
    expect(req.request.body).toEqual({ message: 'hỏi', category_ids: [7], thinking: true });
    req.flush({ ...ENVELOPE_OK, data: ANSWER });
    await result;
    http.verify();
  });

  it('reasoning thiếu ở DTO thì mapper trả chuỗi rỗng chứ không undefined', async () => {
    const result = service.send({ message: 'hỏi' }).toPromise();
    http
      .expectOne(api('/chat'))
      .flush({ ...ENVELOPE_OK, data: { ...ANSWER, reasoning: undefined } });

    expect((await result)!.reasoning).toBe('');
    http.verify();
  });

  /**
   * Biểu đồ (Đợt 26): mapper giữ nguyên spec do backend dựng, KHÔNG đụng vào
   * con số nào. Thiếu `charts` thì trả mảng rỗng — đối xứng với BE (luôn là []).
   */
  it('charts đi từ DTO vào answer, thiếu thì là mảng rỗng', async () => {
    const chart = {
      n: 9,
      kind: 'bar',
      spec: {
        xAxis: { type: 'category', data: ['NV01'] },
        series: [{ type: 'bar', data: [800000] }],
      },
      caption: 'Nguồn: [^9] · Phân bố phụ cấp',
    };
    const withCharts = service.send({ message: 'hỏi' }).toPromise();
    http.expectOne(api('/chat')).flush({ ...ENVELOPE_OK, data: { ...ANSWER, charts: [chart] } });

    const answer = await withCharts;
    expect(answer!.charts).toEqual([
      { n: 9, kind: 'bar', spec: chart.spec, caption: chart.caption },
    ]);

    const withoutCharts = service.send({ message: 'hỏi' }).toPromise();
    http.expectOne(api('/chat')).flush({ ...ENVELOPE_OK, data: ANSWER });
    expect((await withoutCharts)!.charts).toEqual([]);
    http.verify();
  });
});
