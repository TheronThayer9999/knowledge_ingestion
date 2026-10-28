import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { beforeEach, describe, expect, it } from 'vitest';
import { environment } from '../../../environments/environment';
import { toLlmOptions } from './llm.models';
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
});
