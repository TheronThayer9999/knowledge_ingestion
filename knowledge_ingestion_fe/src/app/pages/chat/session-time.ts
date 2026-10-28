import type { Conversation } from '../../core/chat/chat.models';
import type { TranslationKey } from '../../core/i18n/lang.service';

/**
 * Thời gian + chia nhóm cho sidebar phiên (Đợt 27). Tách khỏi component vì hai
 * thứ dễ sai nhất của tính năng này là thuần số học ngày tháng: ngưỡng "hôm
 * qua" và bucket "7 ngày qua" phải tính theo NGÀY LỊCH, không phải 24h trôi —
 * phiên lúc 23h50 phải vào "Hôm qua" chứ không phải "Hôm nay" chỉ vì mới 10 phút.
 */

const MINUTE_MS = 60_000;
const DAY_MS = 86_400_000;
const WEEK_DAYS = 7;

export type Translate = (key: TranslationKey) => string;

export type SessionGroupKey = 'today' | 'yesterday' | 'week' | 'older';

export const SESSION_GROUP_HEADING: Record<SessionGroupKey, TranslationKey> = {
  today: 'CHAT_GROUP_TODAY',
  yesterday: 'CHAT_GROUP_YESTERDAY',
  week: 'CHAT_GROUP_WEEK',
  older: 'CHAT_GROUP_OLDER',
};

const GROUP_ORDER: SessionGroupKey[] = ['today', 'yesterday', 'week', 'older'];

export interface SessionGroup {
  key: SessionGroupKey;
  heading: TranslationKey;
  items: Conversation[];
}

/** Nửa đêm địa phương của mốc — đơn vị để đếm "cách nhau mấy ngày lịch" */
export function startOfDay(date: Date): number {
  const copy = new Date(date);
  copy.setHours(0, 0, 0, 0);
  return copy.getTime();
}

/** Số ngày lịch so với `now`; 0 là hôm nay, 1 là hôm qua, âm (đồng hồ lệch) coi là hôm nay */
export function dayDiff(at: Date, now: Date): number {
  return Math.round((startOfDay(now) - startOfDay(at)) / DAY_MS);
}

/** dd/MM/yyyy — định dạng ngắn gọn dùng cho phiên quá 7 ngày */
export function formatDate(date: Date): string {
  const pad = (value: number): string => String(value).padStart(2, '0');
  return `${pad(date.getDate())}/${pad(date.getMonth() + 1)}/${date.getFullYear()}`;
}

/** i18n của app không có nội suy nên `{n}` thay tại chỗ dùng */
function withCount(template: string, n: number): string {
  return template.replace('{n}', String(n));
}

/** "vừa xong" / "5 phút trước" / "hôm qua" / "2 giờ trước" / "3 ngày trước" / ngày tuyệt đối */
export function timeAgo(at: Date, now: Date, t: Translate): string {
  const minutes = Math.max(0, Math.round((now.getTime() - at.getTime()) / MINUTE_MS));
  if (minutes < 1) {
    return t('CHAT_TIME_NOW');
  }
  if (minutes < 60) {
    return withCount(t('CHAT_TIME_MIN'), minutes);
  }
  // Kiểm ranh giới NGÀY trước ngưỡng 24h: 23h50 hôm qua cách đây 16 tiếng vẫn
  // phải là "hôm qua", không phải "16 giờ trước" chỉ vì chưa đủ một ngày trôi.
  const days = dayDiff(at, now);
  if (days === 1) {
    return t('CHAT_TIME_YESTERDAY');
  }
  if (minutes < 60 * 24) {
    return withCount(t('CHAT_TIME_HOUR'), Math.round(minutes / 60));
  }
  if (days < WEEK_DAYS) {
    return withCount(t('CHAT_TIME_DAY'), Math.max(2, days));
  }
  return formatDate(at);
}

/**
 * Sắp theo hoạt động gần nhất rồi chia 4 nhóm thời gian, bỏ nhóm trống.
 * Không sửa mảng gốc — trang vẫn cầm danh sách thô để cursor keyset.
 */
export function groupSessions(conversations: Conversation[], now: Date): SessionGroup[] {
  const sorted = [...conversations].sort(
    (a, b) => b.lastMessageAt.getTime() - a.lastMessageAt.getTime(),
  );
  const buckets: Record<SessionGroupKey, Conversation[]> = {
    today: [],
    yesterday: [],
    week: [],
    older: [],
  };
  for (const conversation of sorted) {
    const days = dayDiff(conversation.lastMessageAt, now);
    const key: SessionGroupKey =
      days <= 0 ? 'today' : days === 1 ? 'yesterday' : days < WEEK_DAYS ? 'week' : 'older';
    buckets[key].push(conversation);
  }
  return GROUP_ORDER.filter((key) => buckets[key].length > 0).map((key) => ({
    key,
    heading: SESSION_GROUP_HEADING[key],
    items: buckets[key],
  }));
}

/**
 * Nhãn đếm tin nhắn của phiên. `count === 0` vẫn in "0 tin nhắn" — sidebar chỉ
 * gọi khi phiên thật, phiên chưa tạo không có row để đếm.
 */
export function messageCountLabel(count: number, t: Translate): string {
  return withCount(t('CHAT_SESSION_MESSAGES'), count);
}

/**
 * Tiêu đề hiển thị: không bao giờ để trống với phiên đã có tin nhắn (nhóm 1).
 * `messageCount > 0` mà title rỗng chỉ xảy ra với phiên tạo bằng API thô.
 */
export function sessionTitle(conversation: Conversation, t: Translate): string {
  if (conversation.title !== '') {
    return conversation.title;
  }
  return t(conversation.messageCount === 0 ? 'CHAT_UNTITLED' : 'CHAT_SESSION_UNTITLED');
}
