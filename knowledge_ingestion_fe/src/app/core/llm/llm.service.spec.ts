import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { beforeEach, describe, expect, it } from 'vitest';
import { environment } from '../../../environments/environment';
import { toLlmOptions, toProviderKind } from './llm.models';
import { LlmService } from './llm.service';

const api = (path: string): string => `${environment.apiBaseUrl}${path}`;

const ENVELOPE_OK = { status: true, code: 0, message: 'success' };

const OPTIONS_DTO = {
  default_provider: '9router',
  default_model: 'nemotron',
  providers: [
    {
      slug: '9router',
      label: '9Router',
      type: 'openai',
      default_model: 'nemotron',
      models: ['nemotron', 'gpt-oss'],
      available: true,
      thinking: true,
      is_default: true,
      base_url: 'http://localhost:20128/v1',
      has_key: true,
    },
    {
      slug: 'groq',
      type: 'openai',
      default_model: 'llama-3.3-70b',
      available: false,
    },
  ],
};

describe('LlmService', () => {
  let service: LlmService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    service = TestBed.inject(LlmService);
    http = TestBed.inject(HttpTestingController);
  });

  it('gọi GET /llm/options và map về domain model', async () => {
    const result = service.options().toPromise();

    const req = http.expectOne(api('/llm/options'));
    expect(req.request.method).toBe('GET');
    req.flush({ ...ENVELOPE_OK, data: OPTIONS_DTO });

    const options = await result;
    expect(options!.defaultProvider).toBe('9router');
    expect(options!.providers).toHaveLength(2);
    expect(options!.providers[0].models).toEqual(['nemotron', 'gpt-oss']);
    http.verify();
  });

  it('lưu key: PUT đúng body, response chỉ trả cờ nguồn key', async () => {
    const result = service.saveKey('9router', 'sk-1').toPromise();
    const req = http.expectOne({ method: 'PUT', url: api('/llm/providers/9router/key') });
    expect(req.request.body).toEqual({ api_key: 'sk-1' });
    req.flush({
      ...ENVELOPE_OK,
      data: { ...OPTIONS_DTO.providers[0], has_key: true, key_source: 'database' },
    });

    const option = await result;
    expect(option!.hasKey).toBe(true);
    expect(option!.keySource).toBe('database');
    http.verify();
  });

  it('xoá key: DELETE không kèm body', async () => {
    const result = service.clearKey('groq').toPromise();
    const req = http.expectOne({ method: 'DELETE', url: api('/llm/providers/groq/key') });
    expect(req.request.body).toBeNull();
    req.flush({ ...ENVELOPE_OK, data: OPTIONS_DTO.providers[1] });

    const option = await result;
    expect(option!.slug).toBe('groq');
    http.verify();
  });

  it('tải model: POST rồi bóc mảng models', async () => {
    const result = service.loadModels('groq').toPromise();
    const req = http.expectOne({ method: 'POST', url: api('/llm/providers/groq/models') });
    req.flush({ ...ENVELOPE_OK, data: { slug: 'groq', models: ['a', 'b'] } });

    expect(await result).toEqual(['a', 'b']);
    http.verify();
  });

  it('chọn model: PUT body {model}', async () => {
    const result = service.selectModel('groq', 'a').toPromise();
    const req = http.expectOne({ method: 'PUT', url: api('/llm/providers/groq/model') });
    expect(req.request.body).toEqual({ model: 'a' });
    req.flush({ ...ENVELOPE_OK, data: { ...OPTIONS_DTO.providers[1], default_model: 'a' } });

    const option = await result;
    expect(option!.defaultModel).toBe('a');
    http.verify();
  });

  it('slug lạ được encode trước khi ghép vào path', () => {
    service.saveKey('a/b', 'k').subscribe();
    const req = http.expectOne({ method: 'PUT', url: api('/llm/providers/a%2Fb/key') });
    req.flush({ ...ENVELOPE_OK, data: { slug: 'a/b' } });
    http.verify();
  });

  // ---- nấc 3: CRUD định nghĩa provider ----

  it('saveProvider: PUT /llm/providers/{slug}, trả về cả danh sách đã map', async () => {
    const result = service
      .saveProvider('minimax', { label: 'MiniMax', type: 'openai-compatible' })
      .toPromise();
    const req = http.expectOne({ method: 'PUT', url: api('/llm/providers/minimax') });
    expect(req.request.body).toEqual({ label: 'MiniMax', type: 'openai-compatible' });
    req.flush({
      ...ENVELOPE_OK,
      data: {
        ...OPTIONS_DTO,
        providers: [...OPTIONS_DTO.providers, { slug: 'minimax', base_url: 'https://x/v1' }],
      },
    });

    const options = await result;
    expect(options!.providers).toHaveLength(3);
    expect(options!.providers[2].fromConfig).toBe(false);
    expect(options!.providers[2].baseUrl).toBe('https://x/v1');
    http.verify();
  });

  it('deleteProvider: DELETE không body, map danh sách còn lại', async () => {
    const result = service.deleteProvider('minimax').toPromise();
    const req = http.expectOne({ method: 'DELETE', url: api('/llm/providers/minimax') });
    expect(req.request.body).toBeNull();
    req.flush({ ...ENVELOPE_OK, data: OPTIONS_DTO });

    const options = await result;
    expect(options!.providers.map((item) => item.slug)).toEqual(['9router', 'groq']);
    http.verify();
  });
});

describe('toLlmOptions — mapper chịu được DTO cũ/thiếu field', () => {
  it('provider không có label thì rơi về slug', () => {
    const options = toLlmOptions(OPTIONS_DTO);
    expect(options.providers[1].label).toBe('groq');
  });

  it('thiếu models thì là mảng rỗng, không phải undefined', () => {
    const options = toLlmOptions(OPTIONS_DTO);
    expect(options.providers[1].models).toEqual([]);
  });

  it('thiếu boolean thì false — không để undefined lọt vào template', () => {
    const options = toLlmOptions(OPTIONS_DTO);
    expect(options.providers[1].available).toBe(false);
    expect(options.providers[1].thinking).toBe(false);
    expect(options.providers[1].isDefault).toBe(false);
  });

  it('DTO rỗng thì không vỡ template', () => {
    const options = toLlmOptions({});
    expect(options.defaultProvider).toBe('');
    expect(options.providers).toEqual([]);
  });

  it('key_source lạ thì bỏ, config_model giữ nguyên cho admin', () => {
    const options = toLlmOptions({
      providers: [{ slug: 'x', key_source: 'weird', config_model: 'm' }],
    });
    expect(options.providers[0].keySource).toBeUndefined();
    expect(options.providers[0].configModel).toBe('m');
  });

  it('from_config/disabled: chỉ true khi backend trả true', () => {
    const options = toLlmOptions({
      providers: [
        { slug: 'a', from_config: true, disabled: true },
        { slug: 'b' },
        { slug: 'c', from_config: false, disabled: false },
      ],
    });
    expect(options.providers[0].fromConfig).toBe(true);
    expect(options.providers[0].disabled).toBe(true);
    // user thường không nhận hai field này → false, form vẫn hiện hàng
    expect(options.providers[1].fromConfig).toBe(false);
    expect(options.providers[1].disabled).toBe(false);
    expect(options.providers[2].fromConfig).toBe(false);
  });

  it('toProviderKind đảo lại chuẩn hoá của server cho ô select', () => {
    expect(toProviderKind('ollama')).toBe('ollama');
    expect(toProviderKind('openai')).toBe('openai-compatible');
    expect(toProviderKind('')).toBe('openai-compatible');
  });
});
