import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { MatDialogRef } from '@angular/material/dialog';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { environment } from '../../../environments/environment';
import { LangService } from '../../core/i18n/lang.service';
import { MessageService } from '../../shared/feedback/message.service';
import { LlmProvidersDialog } from './llm-providers-dialog';

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
      models: ['nemotron', 'qwen3'],
      available: true,
      thinking: true,
      is_default: true,
      base_url: 'http://localhost:20128/v1',
      has_key: true,
      key_source: 'config',
      config_model: 'nemotron',
    },
    {
      slug: 'groq',
      label: 'Groq',
      type: 'openai',
      default_model: 'llama-3.3-70b',
      models: [],
      available: false,
      is_default: false,
      base_url: 'https://api.groq.com/openai/v1',
      has_key: false,
      key_source: '',
      config_model: 'llama-3.3-70b',
    },
  ],
};

interface Harness {
  fixture: ComponentFixture<LlmProvidersDialog>;
  element: HTMLElement;
  http: HttpTestingController;
  messages: { success: ReturnType<typeof vi.fn> };
}

function setup(options: unknown = OPTIONS_DTO): Harness {
  TestBed.resetTestingModule();
  const messages = { success: vi.fn() };
  TestBed.configureTestingModule({
    imports: [LlmProvidersDialog],
    providers: [
      provideHttpClient(),
      provideHttpClientTesting(),
      { provide: MessageService, useValue: messages },
      { provide: MatDialogRef, useValue: { close: vi.fn() } },
    ],
  });
  const fixture = TestBed.createComponent(LlmProvidersDialog);
  TestBed.inject(LangService).setLang('vi');
  fixture.detectChanges();
  const http = TestBed.inject(HttpTestingController);
  http
    .expectOne(api('/llm/options'))
    .flush({ ...ENVELOPE_OK, data: options as Record<string, unknown> });
  fixture.detectChanges();
  return { fixture, element: fixture.nativeElement as HTMLElement, http, messages };
}

function button(element: HTMLElement, label: string): HTMLButtonElement {
  const found = Array.from(element.querySelectorAll('button')).find((item) =>
    (item.textContent ?? '').includes(label),
  );
  if (!found) {
    throw new Error(`Không tìm thấy nút "${label}"`);
  }
  return found;
}

describe('LlmProvidersDialog — quản trị kết nối AI (Đợt 28)', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('nạp danh sách, mở sẵn provider mặc định và hiện nguồn key', () => {
    const { element } = setup();

    expect(element.textContent).toContain('9Router');
    expect(element.textContent).toContain('Groq');
    expect(element.textContent).toContain('Mặc định');
    expect(element.textContent).toContain('http://localhost:20128/v1');
    expect(element.textContent).toContain('configs/config.json');
    // Chỉ hàng đang mở mới có ô key
    expect(element.querySelectorAll('input[type="password"]').length).toBe(1);
  });

  it('lưu key: gửi đúng body, sau đó chuyển nguồn key sang CSDL và xoá bản nháp', () => {
    const { fixture, element, http, messages } = setup();
    const input = element.querySelector<HTMLInputElement>('input[type="password"]');
    expect(input).not.toBeNull();

    input!.value = 'sk-new-secret';
    input!.dispatchEvent(new Event('input'));
    fixture.detectChanges();

    button(element, 'Lưu key').click();
    const req = http.expectOne({ method: 'PUT', url: api('/llm/providers/9router/key') });
    expect(req.request.body).toEqual({ api_key: 'sk-new-secret' });
    req.flush({
      ...ENVELOPE_OK,
      data: {
        ...OPTIONS_DTO.providers[0],
        has_key: true,
        key_source: 'database',
        available: true,
      },
    });
    fixture.detectChanges();

    expect(messages.success).toHaveBeenCalledWith('SETTINGS_LLM_KEY_SAVED');
    expect(element.textContent).toContain('CSDL (đã mã hoá)');
    expect(button(element, 'Xoá key')).toBeTruthy();
    expect(input!.value).toBe('');
    // Key không bao giờ được render ngược lại
    expect(element.textContent).not.toContain('sk-new-secret');
    http.verify();
  });

  it('xoá key đưa provider về key trong config', () => {
    const options = {
      ...OPTIONS_DTO,
      providers: [
        { ...OPTIONS_DTO.providers[0], key_source: 'database' },
        OPTIONS_DTO.providers[1],
      ],
    };
    const { fixture, element, http, messages } = setup(options);

    button(element, 'Xoá key').click();
    http
      .expectOne({ method: 'DELETE', url: api('/llm/providers/9router/key') })
      .flush({ ...ENVELOPE_OK, data: { ...options.providers[0], key_source: 'config' } });
    fixture.detectChanges();

    expect(messages.success).toHaveBeenCalledWith('SETTINGS_LLM_KEY_CLEARED');
    expect(element.textContent).not.toContain('CSDL (đã mã hoá)');
    http.verify();
  });

  it('tải model: POST tới provider, cập nhật danh sách và cho chọn model mặc định', () => {
    const { fixture, element, http, messages } = setup();
    button(element, 'Groq').click();
    fixture.detectChanges();

    expect(element.textContent).toContain('Chưa có danh sách model');
    button(element, 'Tải model').click();
    const load = http.expectOne({ method: 'POST', url: api('/llm/providers/groq/models') });
    load.flush({ ...ENVELOPE_OK, data: { slug: 'groq', models: ['llama-3.3-70b', 'mixtral'] } });
    fixture.detectChanges();

    expect(messages.success).toHaveBeenCalledWith('SETTINGS_LLM_MODEL_LOADED');
    expect(element.textContent).not.toContain('Chưa có danh sách model');

    button(element, 'Dùng model này').click();
    const select = http.expectOne({ method: 'PUT', url: api('/llm/providers/groq/model') });
    expect(select.request.body).toEqual({ model: 'llama-3.3-70b' });
    select.flush({
      ...ENVELOPE_OK,
      data: {
        ...OPTIONS_DTO.providers[1],
        models: ['llama-3.3-70b', 'mixtral'],
        available: true,
        has_key: true,
        key_source: 'database',
      },
    });
    fixture.detectChanges();

    expect(messages.success).toHaveBeenCalledWith('SETTINGS_LLM_MODEL_SAVED');
    http.verify();
  });

  it('API lỗi khi tải danh sách thì báo lỗi, không vỡ popup', () => {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({
      imports: [LlmProvidersDialog],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        { provide: MessageService, useValue: { success: vi.fn() } },
        { provide: MatDialogRef, useValue: { close: vi.fn() } },
      ],
    });
    const fixture = TestBed.createComponent(LlmProvidersDialog);
    TestBed.inject(LangService).setLang('vi');
    fixture.detectChanges();
    const element = fixture.nativeElement as HTMLElement;

    TestBed.inject(HttpTestingController)
      .expectOne(api('/llm/options'))
      .flush(
        { ...ENVELOPE_OK, status: false, code: 500, message: 'lỗi' },
        { status: 500, statusText: 'Server Error' },
      );
    fixture.detectChanges();

    expect(element.textContent).toContain('Không tải được danh sách nhà cung cấp AI.');
  });

  it('lỗi khi lưu key hiện message server trong banner', () => {
    const { fixture, element, http } = setup();
    const input = element.querySelector<HTMLInputElement>('input[type="password"]')!;
    input.value = 'sk-bad';
    input.dispatchEvent(new Event('input'));
    fixture.detectChanges();

    button(element, 'Lưu key').click();
    http
      .expectOne({ method: 'PUT', url: api('/llm/providers/9router/key') })
      .flush(
        { ...ENVELOPE_OK, status: false, code: 400, message: 'Key không hợp lệ' },
        { status: 400, statusText: 'Bad Request' },
      );
    fixture.detectChanges();

    expect(element.textContent).toContain('Key không hợp lệ');
    http.verify();
  });
});
