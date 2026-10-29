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
  default_model: 'llama-3.3-70b',
  models: ['llama-3.3-70b'],
  available: true,
  thinking: false,
  is_default: false,
  base_url: 'https://api.groq.com/openai/v1',
  has_key: true,
  key_source: 'database',
};

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

/** Ô nhập trong hàng đang mở: [nhãn, base_url, api_key, model]. */
function defInputs(element: HTMLElement): HTMLInputElement[] {
  return Array.from(element.querySelectorAll<HTMLInputElement>('.provider-body .def-row input'));
}

/** Ô nhập trong form "Thêm kết nối": [nhãn, base_url, api_key, model]. */
function createInputs(element: HTMLElement): HTMLInputElement[] {
  return Array.from(element.querySelectorAll<HTMLInputElement>('.create-form input'));
}

function type(input: HTMLInputElement, value: string): void {
  input.value = value;
  input.dispatchEvent(new Event('input'));
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

  it('form thêm kết nối: ẩn tới khi bấm, thiếu base URL thì nút tạo còn tắt', () => {
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

    type(inputs[1], 'https://api.groq.com/openai/v1');
    fixture.detectChanges();
    expect(button(element, 'Tạo kết nối').disabled).toBe(false);
  });

  it('tạo kết nối: POST /llm/my/providers, hàng mới hiện ra và mở sẵn', () => {
    const { fixture, element, http, messages } = setup([]);
    button(element, 'Thêm kết nối').click();
    fixture.detectChanges();

    const inputs = createInputs(element);
    type(inputs[0], 'Groq của tôi');
    type(inputs[1], 'https://api.groq.com/openai/v1');
    type(inputs[2], 'gsk-secret');
    fixture.detectChanges();

    button(element, 'Tạo kết nối').click();
    const req = http.expectOne({ method: 'POST', url: api('/llm/my/providers') });
    expect(req.request.body).toEqual({
      label: 'Groq của tôi',
      type: 'openai-compatible',
      base_url: 'https://api.groq.com/openai/v1',
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

  it('sửa kết nối: gửi đủ nhãn + giao thức + base URL (backend bắt buộc)', () => {
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
      type: 'openai-compatible',
      base_url: 'https://api.groq.com/openai/v1',
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

    const inputs = createInputs(element);
    type(inputs[0], 'Groq của tôi');
    type(inputs[1], 'https://api.groq.com/openai/v1');
    fixture.detectChanges();
    button(element, 'Tạo kết nối').click();

    http
      .expectOne({ method: 'POST', url: api('/llm/my/providers') })
      .flush(
        { ...ENVELOPE_OK, status: false, code: 400, message: 'Kết nối OpenAI-compatible cần API key' },
        { status: 400, statusText: 'Bad Request' },
      );
    fixture.detectChanges();

    expect(element.textContent).toContain('Kết nối OpenAI-compatible cần API key');
    expect(element.querySelector('.create-form')).not.toBeNull();
    expect(messages.success).not.toHaveBeenCalled();
    http.verify();
  });
});
