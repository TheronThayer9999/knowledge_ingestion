import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { beforeEach, describe, expect, it } from 'vitest';
import { environment } from '../../../environments/environment';
import { ApiError } from './api.models';
import { ApiService } from './api.service';

// URL khớp những gì ApiService thực sự gửi đi. apiBaseUrl là tương đối ở bản
// production nhưng tuyệt đối (http://127.0.0.1:9001) ở bản dev — test build
// đang chạy bản dev nên đừng hardcode '/api/v1/...', luôn ghép từ environment.
const api = (path: string): string => `${environment.apiBaseUrl}${path}`;

describe('ApiService', () => {
  let service: ApiService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    service = TestBed.inject(ApiService);
    http = TestBed.inject(HttpTestingController);
  });

  it('bóc data khi envelope thành công', async () => {
    const result = service.get<{ message: string }>('/ping').toPromise();

    const req = http.expectOne(api('/ping'));
    expect(req.request.method).toBe('GET');
    req.flush({ status: true, code: 0, message: 'success', data: { message: 'pong' } });

    await expect(result).resolves.toEqual({ message: 'pong' });
  });

  it('gửi body JSON khi POST', () => {
    service.post('/auth/login', { username: 'a', password: 'b' }).subscribe();

    const req = http.expectOne(api('/auth/login'));
    expect(req.request.body).toEqual({ username: 'a', password: 'b' });
    req.flush({ status: true, code: 0, message: 'success', data: null });
  });

  it('ném ApiError từ envelope status=false', async () => {
    const result = service.get('/users').toPromise();

    const req = http.expectOne(api('/users'));
    req.flush({ status: false, code: 401, message: 'sai tài khoản hoặc mật khẩu', data: null });

    await expect(result).rejects.toMatchObject({
      status: 200,
      code: 401,
      message: 'sai tài khoản hoặc mật khẩu',
    });
  });

  it('map HTTP 401 kèm envelope sang ApiError(401)', async () => {
    const result = service.post('/auth/login', {}).toPromise();

    const req = http.expectOne(api('/auth/login'));
    req.flush(
      { status: false, code: 401, message: 'sai tài khoản hoặc mật khẩu', data: null },
      { status: 401, statusText: 'Unauthorized' },
    );

    await expect(result).rejects.toBeInstanceOf(ApiError);
    await expect(result).rejects.toMatchObject({ status: 401, code: 401 });
  });

  it('map lỗi mạng sang ApiError(0)', async () => {
    const result = service.get('/ping').toPromise();

    http.expectOne(api('/ping')).error(new ProgressEvent('error'));

    await expect(result).rejects.toMatchObject({ status: 0, code: 0 });
  });

  it('ghép query string từ object', () => {
    service.get('/articles', { limit: 10, offset: 0 }).subscribe();

    const req = http.expectOne(api('/articles?limit=10&offset=0'));
    req.flush({ status: true, code: 0, message: 'success', data: [] });
  });
});
