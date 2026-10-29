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

/** Kết nối RIÊNG của tôi (BYOK) — backend sinh slug "u_<id>" */
const MY_CONNECTION = {
  slug: 'u_12',
  id: 12,
  label: 'Groq của tôi',
  type: 'openai',
  default_model: 'llama-3.3-70b',
  models: ['llama-3.3-70b'],
  available: true,
  thinking: false,
  is_default: false,
  base_url: 'https://api.groq.com/openai/v1',
  has_key: true,
  key_source: 'database',
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

  // ---- Kết nối RIÊNG của tôi (BYOK) ----

  it('listMyProviders: GET /llm/my/providers, map sang option có id', async () => {
    const result = service.listMyProviders().toPromise();
    const req = http.expectOne({ method: 'GET', url: api('/llm/my/providers') });
    req.flush({ ...ENVELOPE_OK, data: [MY_CONNECTION] });

    const list = await result;
    expect(list![0].id).toBe(12);
    expect(list![0].slug).toBe('u_12');
    expect(list![0].hasKey).toBe(true);
    http.verify();
  });

  it('saveMyProvider tạo mới: POST /llm/my/providers, sửa thì PUT /llm/my/providers/{id}', async () => {
    const created = service
      .saveMyProvider(0, { label: 'Groq của tôi', type: 'openai-compatible', base_url: 'https://x/v1' })
      .toPromise();
    const create = http.expectOne({ method: 'POST', url: api('/llm/my/providers') });
    expect(create.request.body).toEqual({
      label: 'Groq của tôi',
      type: 'openai-compatible',
      base_url: 'https://x/v1',
    });
    create.flush({ ...ENVELOPE_OK, data: MY_CONNECTION });

    const updated = service
      .saveMyProvider(12, { label: 'Groq của tôi', type: 'openai-compatible', base_url: 'https://x/v1' })
      .toPromise();
    const update = http.expectOne({ method: 'PUT', url: api('/llm/my/providers/12') });
    expect(update.request.body.label).toBe('Groq của tôi');
    update.flush({ ...ENVELOPE_OK, data: MY_CONNECTION });

    expect((await created)!.slug).toBe('u_12');
    expect((await updated)!.id).toBe(12);
    http.verify();
  });

  it('deleteMyProvider: DELETE trả về danh sách còn lại', async () => {
    const result = service.deleteMyProvider(12).toPromise();
    const req = http.expectOne({ method: 'DELETE', url: api('/llm/my/providers/12') });
    expect(req.request.body).toBeNull();
    req.flush({ ...ENVELOPE_OK, data: [] });

    expect(await result).toEqual([]);
    http.verify();
  });

  it('loadMyProviderModels: POST tới provider của tôi rồi bóc mảng models', async () => {
    const result = service.loadMyProviderModels(12).toPromise();
    const req = http.expectOne({ method: 'POST', url: api('/llm/my/providers/12/models') });
    req.flush({ ...ENVELOPE_OK, data: { slug: 'u_12', models: ['a', 'b'] } });

    expect(await result).toEqual(['a', 'b']);
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
    expect(options.myProviders).toEqual([]);
  });

  it('my_providers map sang danh sách kết nối riêng (có id)', () => {
    const options = toLlmOptions({ my_providers: [MY_CONNECTION] });
    expect(options.myProviders).toHaveLength(1);
    expect(options.myProviders[0].id).toBe(12);
    expect(options.myProviders[0].slug).toBe('u_12');
    expect(options.myProviders[0].label).toBe('Groq của tôi');
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
