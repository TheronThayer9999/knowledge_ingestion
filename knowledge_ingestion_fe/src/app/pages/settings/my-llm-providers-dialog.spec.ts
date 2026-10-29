import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { MatDialogRef } from '@angular/material/dialog';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { environment } from '../../../environments/environment';
import { LangService } from '../../core/i18n/lang.service';
import { MessageService } from '../../shared/feedback/message.service';
import { MyLlmProvidersDialog } from './my-llm-providers-dialog';

const api = (path: string): string => `${environment.apiBaseUrl}${path}`;

const ENVELOPE_OK = { status: true, code: 0, message: 'success' };

/** Kết nối riêng — backend tự sinh slug "u_<id>", không có slug do user chọn. */
const MY_CONNECTION = {
  slug: 'u_12',
  id: 12,
  label: 'Groq của tôi',
  type: 'openai',
  provider: 'groq',
  default_model: 'llama-3.3-70b',
  models: ['llama-3.3-70b'],
  available: true,
  thinking: false,
  is_default: false,
  has_key: true,
  key_source: 'database',
};

/** Danh mục provider hệ thống — user chọn ở đây rồi dán key của họ. */
const CATALOG = [
  { slug: 'groq', label: 'Groq', type: 'openai' },
  { slug: 'ollama', label: 'Ollama local', type: 'ollama' },
];

interface Harness {
  fixture: ComponentFixture<MyLlmProvidersDialog>;
  element: HTMLElement;
  http: HttpTestingController;
  messages: { success: ReturnType<typeof vi.fn> };
}

function setup(connections: unknown[] = [MY_CONNECTION]): Harness {
  TestBed.resetTestingModule();
  const messages = { success: vi.fn() };
  TestBed.configureTestingModule({
    imports: [MyLlmProvidersDialog],
    providers: [
      provideHttpClient(),
      provideHttpClientTesting(),
      { provide: MessageService, useValue: messages },
      { provide: MatDialogRef, useValue: { close: vi.fn() } },
    ],
  });
  const fixture = TestBed.createComponent(MyLlmProvidersDialog);
  TestBed.inject(LangService).setLang('vi');
  fixture.detectChanges();
  const http = TestBed.inject(HttpTestingController);
  http
    .expectOne({ method: 'GET', url: api('/llm/my/providers') })
    .flush({ ...ENVELOPE_OK, data: connections });
  http
    .expectOne({ method: 'GET', url: api('/llm/my/providers/catalog') })
    .flush({ ...ENVELOPE_OK, data: CATALOG });
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

/** Ô nhập trong hàng đang mở: [nhãn, api_key, model]. */
function defInputs(element: HTMLElement): HTMLInputElement[] {
  return Array.from(element.querySelectorAll<HTMLInputElement>('.provider-body .def-row input'));
}

/** Ô nhập trong form "Thêm kết nối": [nhãn, api_key, model]. */
function createInputs(element: HTMLElement): HTMLInputElement[] {
  return Array.from(element.querySelectorAll<HTMLInputElement>('.create-form input'));
}

function type(input: HTMLInputElement, value: string): void {
  input.value = value;
  input.dispatchEvent(new Event('input'));
}

/** Chọn provider trong select của form tạo mới (mat-select render option overlay). */
function pickProvider(element: HTMLElement, label: string): void {
  const trigger = element.querySelector<HTMLElement>('.create-form mat-select');
  if (!trigger) {
    throw new Error('Không tìm thấy ô chọn nhà cung cấp');
  }
  trigger.click();
  const option = Array.from(document.querySelectorAll('mat-option')).find((item) =>
    (item.textContent ?? '').includes(label),
  );
  if (!option) {
    throw new Error(`Không tìm thấy lựa chọn "${label}"`);
  }
  (option as HTMLElement).click();
}

describe('MyLlmProvidersDialog — kết nối LLM riêng của tôi (BYOK)', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('nạp danh sách kết nối riêng, hiện nhãn + slug do backend sinh', () => {
    const { element } = setup();

    expect(element.textContent).toContain('Groq của tôi');
    expect(element.textContent).toContain('u_12');
    // Key KHÔNG BAO GIỜ được render ngược lại — chỉ có cờ "đã có key"
    expect(element.textContent).toContain('Đã có key (đã mã hoá)');
    expect(element.textContent).not.toContain('gsk-');
  });

  it('chưa có kết nối nào thì báo rõ, không vỡ popup', () => {
    const { element } = setup([]);

    expect(element.textContent).toContain('Bạn chưa thêm kết nối AI riêng nào.');
  });

  it('form thêm kết nối: chọn provider + nhập key thì mới cho tạo', () => {
    const { fixture, element } = setup();

    expect(element.querySelector('.create-form')).toBeNull();
    button(element, 'Thêm kết nối').click();
    fixture.detectChanges();

    expect(element.querySelector('.create-form')).not.toBeNull();
    expect(button(element, 'Tạo kết nối').disabled).toBe(true);

    const inputs = createInputs(element);
    type(inputs[0], 'Groq của tôi');
    fixture.detectChanges();
    expect(button(element, 'Tạo kết nối').disabled).toBe(true);

    pickProvider(element, 'Groq');
    fixture.detectChanges();
    expect(button(element, 'Tạo kết nối').disabled).toBe(true);

    type(inputs[1], 'gsk-secret');
    fixture.detectChanges();
    expect(button(element, 'Tạo kết nối').disabled).toBe(false);
  });

  it('tạo kết nối: POST /llm/my/providers với provider đã chọn, không có base URL', () => {
    const { fixture, element, http, messages } = setup([]);
    button(element, 'Thêm kết nối').click();
    fixture.detectChanges();

    pickProvider(element, 'Groq');
    const inputs = createInputs(element);
    type(inputs[0], 'Groq của tôi');
    type(inputs[1], 'gsk-secret');
    fixture.detectChanges();

    button(element, 'Tạo kết nối').click();
    const req = http.expectOne({ method: 'POST', url: api('/llm/my/providers') });
    expect(req.request.body).toEqual({
      label: 'Groq của tôi',
      provider: 'groq',
      api_key: 'gsk-secret',
    });
    req.flush({ ...ENVELOPE_OK, data: MY_CONNECTION });
    fixture.detectChanges();

    expect(messages.success).toHaveBeenCalledWith('SETTINGS_LLM_MY_CREATED');
    expect(element.querySelector('.create-form')).toBeNull();
    expect(element.querySelectorAll('.provider')).toHaveLength(1);
    expect(element.textContent).toContain('Groq của tôi');
    http.verify();
  });

  it('sửa kết nối: chỉ gửi nhãn + key + model, không gửi base URL', () => {
    const { fixture, element, http, messages } = setup();

    const [labelInput] = defInputs(element);
    expect(labelInput.value).toBe('Groq của tôi');
    type(labelInput, 'Groq free');
    fixture.detectChanges();

    expect(button(element, 'Lưu thay đổi').disabled).toBe(false);
    button(element, 'Lưu thay đổi').click();
    const req = http.expectOne({ method: 'PUT', url: api('/llm/my/providers/12') });
    expect(req.request.body).toEqual({
      label: 'Groq free',
      provider: 'groq',
      model: 'llama-3.3-70b',
    });
    req.flush({ ...ENVELOPE_OK, data: { ...MY_CONNECTION, label: 'Groq free' } });
    fixture.detectChanges();

    expect(messages.success).toHaveBeenCalledWith('SETTINGS_LLM_MY_SAVED');
    expect(element.textContent).toContain('Groq free');
    http.verify();
  });

  it('xoá kết nối: DELETE /llm/my/providers/{id}, danh sách cập nhật', () => {
    const { fixture, element, http, messages } = setup();

    button(element, 'Xoá kết nối').click();
    const req = http.expectOne({ method: 'DELETE', url: api('/llm/my/providers/12') });
    req.flush({ ...ENVELOPE_OK, data: [] });
    fixture.detectChanges();

    expect(messages.success).toHaveBeenCalledWith('SETTINGS_LLM_MY_DELETED');
    expect(element.querySelectorAll('.provider')).toHaveLength(0);
    http.verify();
  });

  it('tải model: POST tới provider của tôi rồi đổ danh sách model', () => {
    const { fixture, element, http, messages } = setup();

    button(element, 'Tải model').click();
    const load = http.expectOne({ method: 'POST', url: api('/llm/my/providers/12/models') });
    load.flush({ ...ENVELOPE_OK, data: { slug: 'u_12', models: ['llama-3.3-70b', 'mixtral'] } });
    fixture.detectChanges();

    expect(messages.success).toHaveBeenCalledWith('SETTINGS_LLM_MODEL_LOADED');
    expect(element.textContent).not.toContain('Chưa có danh sách model');
    http.verify();
  });

  it('server từ chối: hiện message của server, không báo thành công', () => {
    const { fixture, element, http, messages } = setup();
    button(element, 'Thêm kết nối').click();
    fixture.detectChanges();

    pickProvider(element, 'Groq');
    const inputs = createInputs(element);
    type(inputs[0], 'Groq của tôi');
    type(inputs[1], 'gsk-secret');
    fixture.detectChanges();
    button(element, 'Tạo kết nối').click();

    http
      .expectOne({ method: 'POST', url: api('/llm/my/providers') })
      .flush(
        { ...ENVELOPE_OK, status: false, code: 400, message: 'Kết nối Groq cần API key' },
        { status: 400, statusText: 'Bad Request' },
      );
    fixture.detectChanges();

    expect(element.textContent).toContain('Kết nối Groq cần API key');
    expect(element.querySelector('.create-form')).not.toBeNull();
    expect(messages.success).not.toHaveBeenCalled();
    http.verify();
  });
});
