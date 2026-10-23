import type { TranslationKey } from '../../core/i18n/lang.service';

export type MessageType = 'success' | 'error' | 'info' | 'warning';

/**
 * Một mẩu feedback đã hiển thị. `text` là chuỗi đã dịch (hoặc message
 * server trả về); `key` giữ lại key i18n khi nguồn là bản dịch —
 * history dùng để debug/audit, không hiển thị lại.
 */
export interface AppMessage {
  type: MessageType;
  text: string;
  key?: TranslationKey;
  at: Date;
}
