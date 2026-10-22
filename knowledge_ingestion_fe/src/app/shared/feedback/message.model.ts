import { TranslationKey, TranslationParams } from '../i18n/lang.model';

export type MessageType = 'success' | 'error' | 'info' | 'warning';

/**
 * Một message "bắn ra" từ trung tâm MessageService.
 * Chỉ chứa key i18n + params — việc dịch sang ngôn ngữ hiện hành do
 * MessageService/LangService lo, nơi phát không giữ text cứng.
 */
export interface AppMessage {
  type: MessageType;
  key: TranslationKey;
  params?: TranslationParams;
}
