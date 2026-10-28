import type { Conversation } from '../../core/chat/chat.models';
import type { TranslationKey } from '../../core/i18n/lang.service';
import {
  dayDiff,
  formatDate,
  groupSessions,
  messageCountLabel,
  sessionTitle,
  timeAgo,
} from './session-time';

/** Trả đúng key làm nhãn: test chỉ cần biết nhánh nào được chọn */
const passthrough = (key: TranslationKey): string => key;
const withTemplate =
  (templates: Partial<Record<TranslationKey, string>>) =>
  (key: TranslationKey): string =>
    templates[key] ?? key;

const NOW = new Date('2026-10-27T15:30:00+07:00');

function minutesAgo(minutes: number): Date {
  return new Date(NOW.getTime() - minutes * 60_000);
}

function daysAgo(days: number, hour = 10): Date {
  const date = new Date(NOW);
  date.setDate(date.getDate() - days);
  date.setHours(hour, 0, 0, 0);
  return date;
}

function makeSession(overrides: Partial<Conversation> = {}): Conversation {
  return {
    id: 1,
    title: 'Quy định phụ cấp',
    provider: '',
    model: '',
    thinking: false,
    categoryIds: [],
    messageCount: 2,
    lastMessageAt: NOW,
    createdAt: NOW,
    ...overrides,
  };
}

describe('timeAgo — mốc tương đối theo yêu cầu', () => {
  it('dưới một phút là "vừa xong"', () => {
    expect(timeAgo(minutesAgo(0), NOW, passthrough)).toBe('CHAT_TIME_NOW');
  });

  it('vài phút thì đếm phút', () => {
    expect(timeAgo(minutesAgo(5), NOW, passthrough)).toBe('CHAT_TIME_MIN');
  });

  it('trong ngày thì đếm giờ', () => {
    expect(timeAgo(minutesAgo(150), NOW, passthrough)).toBe('CHAT_TIME_HOUR');
  });

  it('đúng hôm qua thì nói "hôm qua" chứ không "1 ngày trước"', () => {
    // 23h50 hôm qua, mới hơn 16 tiếng: vẫn là HÔM QUA vì tính theo ngày lịch
    const yesterdayLate = new Date('2026-10-26T23:50:00+07:00');
    expect(timeAgo(yesterdayLate, NOW, passthrough)).toBe('CHAT_TIME_YESTERDAY');
  });

  it('2..6 ngày thì đếm ngày', () => {
    expect(timeAgo(daysAgo(3), NOW, passthrough)).toBe('CHAT_TIME_DAY');
  });

  it('quá 7 ngày in ngày tuyệt đối', () => {
    expect(timeAgo(daysAgo(40), NOW, passthrough)).toBe(formatDate(daysAgo(40)));
  });

  it('nội suy {n} vào nhãn số', () => {
    const t = withTemplate({ CHAT_TIME_MIN: '{n} phút trước' });
    expect(timeAgo(minutesAgo(5), NOW, t)).toBe('5 phút trước');
  });
});

describe('dayDiff — ranh giới ngày lịch, không phải 24h', () => {
  it('23h50 hôm trước là 1 ngày, 00h10 hôm nay là 0 ngày', () => {
    expect(dayDiff(new Date('2026-10-26T23:50:00+07:00'), NOW)).toBe(1);
    expect(dayDiff(new Date('2026-10-27T00:10:00+07:00'), NOW)).toBe(0);
  });

  it('mốc tương lai (đồng hồ lệch) coi là hôm nay', () => {
    expect(dayDiff(new Date(NOW.getTime() + 3_600_000), NOW)).toBeLessThanOrEqual(0);
  });
});

describe('groupSessions — hôm nay / hôm qua / 7 ngày qua / cũ hơn', () => {
  it('sắp theo hoạt động gần nhất rồi chia bucket', () => {
    const groups = groupSessions(
      [
        makeSession({ id: 3, lastMessageAt: daysAgo(60) }),
        makeSession({ id: 1, lastMessageAt: minutesAgo(2) }),
        makeSession({ id: 2, lastMessageAt: daysAgo(1) }),
        makeSession({ id: 4, lastMessageAt: daysAgo(4) }),
      ],
      NOW,
    );
    expect(groups.map((group) => group.key)).toEqual(['today', 'yesterday', 'week', 'older']);
    expect(groups.map((group) => group.heading)).toEqual([
      'CHAT_GROUP_TODAY',
      'CHAT_GROUP_YESTERDAY',
      'CHAT_GROUP_WEEK',
      'CHAT_GROUP_OLDER',
    ]);
    expect(groups[0].items.map((item) => item.id)).toEqual([1]);
    expect(groups[2].items.map((item) => item.id)).toEqual([4]);
  });

  it('bucket rỗng bị loại hẳn khỏi danh sách', () => {
    const groups = groupSessions([makeSession({ lastMessageAt: minutesAgo(3) })], NOW);
    expect(groups.length).toBe(1);
    expect(groups[0].key).toBe('today');
  });

  it('không sửa thứ tự mảng gốc', () => {
    const source = [makeSession({ id: 9, lastMessageAt: daysAgo(30) }), makeSession({ id: 8 })];
    groupSessions(source, NOW);
    expect(source.map((item) => item.id)).toEqual([9, 8]);
  });

  it('danh sách rỗng không có nhóm nào', () => {
    expect(groupSessions([], NOW)).toEqual([]);
  });
});

describe('nhãn đếm tin nhắn và tiêu đề fallback', () => {
  it('đếm bằng "tin nhắn", không còn "lượt"', () => {
    const t = withTemplate({ CHAT_SESSION_MESSAGES: '{n} tin nhắn' });
    expect(messageCountLabel(4, t)).toBe('4 tin nhắn');
  });

  it('phiên chưa có tin nhắn thì "Chưa đặt tên", đã có thì "Cuộc trò chuyện không tên"', () => {
    expect(sessionTitle(makeSession({ title: '', messageCount: 0 }), passthrough)).toBe(
      'CHAT_UNTITLED',
    );
    expect(sessionTitle(makeSession({ title: '', messageCount: 6 }), passthrough)).toBe(
      'CHAT_SESSION_UNTITLED',
    );
  });

  it('có title thì in nguyên văn', () => {
    expect(sessionTitle(makeSession({ title: 'So sánh hai tài liệu' }), passthrough)).toBe(
      'So sánh hai tài liệu',
    );
  });
});
