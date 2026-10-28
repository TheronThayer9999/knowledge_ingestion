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

const ROUTER = {
  slug: '9router',
  label: '9Router',
  type: 'openai',
  default_model: 'nemotron',
  models: ['nemotron', 'qwen3'],
  available: true,
  thinking: true,
  is_default: true,
  from_config: true,
  base_url: 'http://localhost:20128/v1',
  has_key: true,
  key_source: 'config',
  config_model: 'nemotron',
};

const GROQ = {
  slug: 'groq',
  label: 'Groq',
  type: 'openai',
  default_model: 'llama-3.3-70b',
  models: [],
  available: false,
  is_default: false,
  from_config: true,
  base_url: 'https://api.groq.com/openai/v1',
  has_key: false,
  key_source: '',
  config_model: 'llama-3.3-70b',
};

/** Provider chỉ có trong CSDL (admin tạo từ popup) — from_config không được gửi */
const MINIMAX = {
  slug: 'minimax',
  label: 'MiniMax',
  type: 'openai',
  default_model: '',
  models: [],
  available: false,
  is_default: false,
  base_url: 'https://api.minimax.example/v1',
  key_source: '',
};

const OPTIONS_DTO = {
  default_provider: '9router',
  default_model: 'nemotron',
  providers: [ROUTER, GROQ],
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

/** Ô nhập của form định nghĩa trong hàng đang mở: [label, base_url]. */
function defInputs(element: HTMLElement): HTMLInputElement[] {
  return Array.from(element.querySelectorAll<HTMLInputElement>('.provider-body .def-row input'));
}

function type(input: HTMLInputElement, value: string): void {
  input.value = value;
  input.dispatchEvent(new Event('input'));
}

/** Body các endpoint CRUD provider đều là cả danh sách provider sau khi sửa. */
function listEnvelope(providers: unknown[]): Record<string, unknown> {
  return { ...ENVELOPE_OK, data: { ...OPTIONS_DTO, providers } };
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
    expect(element.textContent).toContain('configs/config.json');
    // base_url là ô nhập (admin sửa được), không còn là chữ trong meta
    expect(defInputs(element)[1].value).toBe('http://localhost:20128/v1');
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
        ...ROUTER,
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
      providers: [{ ...ROUTER, key_source: 'database' }, GROQ],
    };
    const { fixture, element, http, messages } = setup(options);

    button(element, 'Xoá key').click();
    http
      .expectOne({ method: 'DELETE', url: api('/llm/providers/9router/key') })
      .flush({ ...ENVELOPE_OK, data: { ...ROUTER, key_source: 'config' } });
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
        ...GROQ,
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

  // ---- nấc 3: định nghĩa provider sửa ngay trong popup ----

  it('form thêm provider: ẩn tới khi bấm, slug sai thì nút tạo tắt', () => {
    const { fixture, element } = setup();

    expect(element.querySelector('.create-form')).toBeNull();
    button(element, 'Thêm provider').click();
    fixture.detectChanges();

    expect(element.querySelector('.create-form')).not.toBeNull();
    expect(button(element, 'Tạo kết nối').disabled).toBe(true);

    const inputs = Array.from(element.querySelectorAll<HTMLInputElement>('.create-form input'));
    type(inputs[0], 'Groq/key');
    fixture.detectChanges();
    expect(button(element, 'Tạo kết nối').disabled).toBe(true);

    type(inputs[0], 'minimax');
    fixture.detectChanges();
    expect(button(element, 'Tạo kết nối').disabled).toBe(false);
  });

  it('tạo provider: PUT /llm/providers/{slug} với loại mặc định, hàng mới hiện ra', () => {
    const { fixture, element, http, messages } = setup();
    button(element, 'Thêm provider').click();
    fixture.detectChanges();

    const inputs = Array.from(element.querySelectorAll<HTMLInputElement>('.create-form input'));
    type(inputs[0], 'minimax');
    type(inputs[1], 'MiniMax');
    fixture.detectChanges();

    button(element, 'Tạo kết nối').click();
    const req = http.expectOne({ method: 'PUT', url: api('/llm/providers/minimax') });
    // Loại để mặc định openai-compatible: form không cần bấm select vẫn tạo được
    // provider Groq/OpenRouter-style, body chỉ mang field admin nhập
    expect(req.request.body).toEqual({ label: 'MiniMax', type: 'openai-compatible' });
    req.flush(listEnvelope([ROUTER, GROQ, MINIMAX]));
    fixture.detectChanges();

    expect(messages.success).toHaveBeenCalledWith('SETTINGS_LLM_PROVIDER_CREATED');
    expect(element.querySelector('.create-form')).toBeNull();
    expect(element.querySelectorAll('.provider')).toHaveLength(3);
    // Hàng mới tạo mở sẵn và là provider "của UI", không có trong file
    expect(element.textContent).toContain('Tạo từ UI (CSDL)');
    expect(element.textContent).toContain('Xoá là mất hẳn kết nối này');
    http.verify();
  });

  it('sửa nhãn: chỉ gửi field thay đổi, không đụng type/base_url', () => {
    const { fixture, element, http, messages } = setup();
    button(element, 'Groq').click();
    fixture.detectChanges();

    const [labelInput] = defInputs(element);
    type(labelInput, 'Groq Free');
    fixture.detectChanges();

    expect(button(element, 'Lưu định nghĩa').disabled).toBe(false);
    button(element, 'Lưu định nghĩa').click();
    const req = http.expectOne({ method: 'PUT', url: api('/llm/providers/groq') });
    expect(req.request.body).toEqual({ label: 'Groq Free' });
    req.flush(listEnvelope([ROUTER, { ...GROQ, label: 'Groq Free' }]));
    fixture.detectChanges();

    expect(messages.success).toHaveBeenCalledWith('SETTINGS_LLM_PROVIDER_SAVED');
    expect(element.textContent).toContain('Groq Free');
    http.verify();
  });

  it('tắt rồi bật lại provider: chỉ gửi công tắc disabled', () => {
    const { fixture, element, http } = setup();

    button(element, 'Tắt kết nối').click();
    const off = http.expectOne({ method: 'PUT', url: api('/llm/providers/9router') });
    expect(off.request.body).toEqual({ disabled: true });
    off.flush({
      ...ENVELOPE_OK,
      data: { ...OPTIONS_DTO, providers: [{ ...ROUTER, disabled: true, available: false }, GROQ] },
    });
    fixture.detectChanges();
    expect(element.textContent).toContain('Đã tắt');

    button(element, 'Bật lại').click();
    const on = http.expectOne({ method: 'PUT', url: api('/llm/providers/9router') });
    expect(on.request.body).toEqual({ disabled: false });
    on.flush({ ...ENVELOPE_OK, data: OPTIONS_DTO });
    fixture.detectChanges();
    expect(element.textContent).not.toContain('Đã tắt');
    http.verify();
  });

  it('xoá provider tạo từ UI: hint nói mất hẳn và DELETE nạp lại danh sách', () => {
    const { fixture, element, http, messages } = setup({
      ...OPTIONS_DTO,
      providers: [ROUTER, GROQ, MINIMAX],
    });

    button(element, 'MiniMax').click();
    fixture.detectChanges();
    expect(element.textContent).toContain('Xoá là mất hẳn kết nối này');

    button(element, 'Xoá').click();
    const req = http.expectOne({ method: 'DELETE', url: api('/llm/providers/minimax') });
    req.flush({ ...ENVELOPE_OK, data: { ...OPTIONS_DTO, providers: [ROUTER, GROQ] } });
    fixture.detectChanges();

    expect(messages.success).toHaveBeenCalledWith('SETTINGS_LLM_PROVIDER_DELETED');
    expect(element.querySelectorAll('.provider')).toHaveLength(2);
    http.verify();
  });

  it('provider khai ở file: hint nói xoá là quay về file', () => {
    const { element } = setup();

    expect(element.textContent).toContain('Xoá là quay về đúng cấu hình trong file');
  });

  it('hàng provider tạo từ UI: hiện nguồn CSDL và hint xoá mất hẳn', () => {
    const { fixture, element } = setup({
      ...OPTIONS_DTO,
      providers: [ROUTER, GROQ, { ...MINIMAX, type: 'ollama' }],
    });

    button(element, 'MiniMax').click();
    fixture.detectChanges();

    expect(element.textContent).toContain('Tạo từ UI (CSDL)');
    expect(element.textContent).toContain('Xoá là mất hẳn kết nối này');
    expect(defInputs(element)[1].value).toBe('https://api.minimax.example/v1');
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

  it('server từ chối tạo provider: giữ form mở và báo lỗi của server', () => {
    const { fixture, element, http, messages } = setup();
    button(element, 'Thêm provider').click();
    fixture.detectChanges();

    const inputs = Array.from(element.querySelectorAll<HTMLInputElement>('.create-form input'));
    type(inputs[0], 'dup');
    fixture.detectChanges();
    button(element, 'Tạo kết nối').click();

    http
      .expectOne({ method: 'PUT', url: api('/llm/providers/dup') })
      .flush(
        { ...ENVELOPE_OK, status: false, code: 400, message: 'Provider "dup" không hợp lệ' },
        { status: 400, statusText: 'Bad Request' },
      );
    fixture.detectChanges();

    expect(element.textContent).toContain('Provider "dup" không hợp lệ');
    expect(element.querySelector('.create-form')).not.toBeNull();
    expect(messages.success).not.toHaveBeenCalled();
    http.verify();
  });
});
