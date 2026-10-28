import {
  clipSessionTitle,
  toChatAnswer,
  toCreateConversationBody,
  toSavedMessage,
} from './chat.models';

describe('clipSessionTitle — tự đặt tên phiên từ câu hỏi đầu', () => {
  it('câu ngắn giữ nguyên, chỉ dọn khoảng trắng thừa', () => {
    expect(clipSessionTitle('  Quy định   phụ cấp  ')).toBe('Quy định phụ cấp');
  });

  it('cắt ở dấu cách gần trần 40 rune nhất, không xé đôi chữ', () => {
    // "của" kết thúc đúng rune 40 nên vẫn vừa — từ kế tiếp mới bị cắt
    expect(
      clipSessionTitle('Nội dung quy chế về phụ cấp công tác của nhân viên khối văn phòng'),
    ).toBe('Nội dung quy chế về phụ cấp công tác của…');
    expect(
      clipSessionTitle('So sánh chi tiết giữa hai quy định về chế độ làm việc và nghỉ phép'),
    ).toBe('So sánh chi tiết giữa hai quy định về…');
  });

  it('tiếng Việt có dấu đếm theo rune — không cắt giữa âm tiết', () => {
    const title = clipSessionTitle('Những quyền lợi nào tôi nên biết về bảo hiểm xã hội hiện nay');
    expect(title).toBe('Những quyền lợi nào tôi nên biết về bảo…');
    expect(Array.from(title).length).toBeLessThanOrEqual(41); // 40 rune + dấu …
  });

  it('bỏ dấu câu treo cuối để title không kết thúc kiểu câu dở', () => {
    expect(clipSessionTitle('aaaa bbbb cccc dddd eeee ffff gggg hhhh, iiii')).toBe(
      'aaaa bbbb cccc dddd eeee ffff gggg hhhh…',
    );
  });

  it('từ đầu tiên đã dài hơn trần thì cắt cứng ở 40', () => {
    expect(clipSessionTitle('a'.repeat(60))).toBe(`${'a'.repeat(40)}…`);
  });

  it('câu rỗng trả chuỗi rỗng để backend giữ quyền điền title', () => {
    expect(clipSessionTitle('   ')).toBe('');
  });
});

describe('toCreateConversationBody — title FE sinh ra được gửi lên', () => {
  it('có title thì đưa vào body snake_case', () => {
    expect(toCreateConversationBody({ title: 'Phụ cấp công tác' })).toEqual({
      title: 'Phụ cấp công tác',
    });
  });

  it('title rỗng thì KHÔNG gửi field (để backend điền sau)', () => {
    expect(toCreateConversationBody({ title: '', categoryIds: [] })).toEqual({});
  });

  it('provider/model có chọn thì gửi lên (Đợt 28)', () => {
    expect(toCreateConversationBody({ provider: 'groq', model: 'llama-3.3-70b' })).toEqual({
      provider: 'groq',
      model: 'llama-3.3-70b',
    });
  });

  it('provider/model rỗng thì KHÔNG gửi — gửi "" sẽ ghi đè lựa chọn của phiên', () => {
    expect(toCreateConversationBody({ provider: '', model: '' })).toEqual({});
  });
});

describe('provider/model đi kèm câu trả lời (Đợt 28)', () => {
  it('response có provider/model thì map vào ChatAnswer', () => {
    const answer = toChatAnswer({
      answer: 'Xin chào',
      question_type: 'general',
      provider: '9router',
      model: 'nemotron',
    });
    expect(answer.provider).toBe('9router');
    expect(answer.model).toBe('nemotron');
  });

  it('backend cũ không có field thì rỗng, không lỗi', () => {
    const answer = toChatAnswer({ answer: 'Xin chào', question_type: 'general' });
    expect(answer.provider).toBe('');
    expect(answer.model).toBe('');
  });

  it('message đã lưu mang provider/model của chính lượt đó', () => {
    const message = toSavedMessage({
      id: 7,
      role: 'assistant',
      content: 'Đáp án',
      provider: 'groq',
      model: 'llama-3.3-70b',
      created_at: '2026-10-28T00:00:00Z',
    });
    expect(message.provider).toBe('groq');
    expect(message.model).toBe('llama-3.3-70b');
  });
});
