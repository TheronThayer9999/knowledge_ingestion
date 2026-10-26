import { articleSource, matchesArticleQuery, type Article } from './knowledge.models';

/** Bài tối thiểu — chỉ cần đủ field của `Article`, test chỉ xét nguồn/tên */
function makeArticle(overrides: Partial<Article> = {}): Article {
  return {
    id: 1,
    url: '',
    storageKey: '',
    name: 'Quy chế nội bộ',
    contentType: 'application/pdf',
    docType: '',
    description: '',
    categoryId: 1,
    extractStatus: 'done',
    indexStatus: 'done',
    compileStatus: 'done',
    createdAt: '2026-10-26T08:00:00+07:00',
    updatedAt: '2026-10-26T08:00:00+07:00',
    ...overrides,
  };
}

describe('articleSource — định danh file để đối chiếu với log', () => {
  it('bài upload trả storage key', () => {
    expect(articleSource(makeArticle({ storageKey: 'uploads/a.pdf' }))).toBe('uploads/a.pdf');
  });

  it('bài link ngoài trả URL', () => {
    expect(articleSource(makeArticle({ url: 'https://example.com/b' }))).toBe(
      'https://example.com/b',
    );
  });

  it('có cả hai thì ưu tiên storage key — khớp fileTag bên Go', () => {
    const both = makeArticle({ url: 'https://example.com/b', storageKey: 'uploads/a.pdf' });
    expect(articleSource(both)).toBe('uploads/a.pdf');
  });

  it('bài không có nguồn trả chuỗi rỗng để template ẩn dòng phụ', () => {
    expect(articleSource(makeArticle())).toBe('');
  });
});

describe('matchesArticleQuery — tìm theo tên hoặc file', () => {
  const uploaded = makeArticle({ name: 'Quy chế nội bộ', storageKey: 'uploads/01a1-abc.pdf' });

  it('khớp theo tên tài liệu', () => {
    expect(matchesArticleQuery(uploaded, 'quy chế')).toBe(true);
  });

  it('khớp theo storage key dán từ dòng log', () => {
    expect(matchesArticleQuery(uploaded, '01a1-abc')).toBe(true);
  });

  it('khớp theo URL với bài link ngoài', () => {
    const linked = makeArticle({ url: 'https://example.com/kien-truc' });
    expect(matchesArticleQuery(linked, 'kien-truc')).toBe(true);
  });

  it('từ khóa rỗng hoặc toàn khoảng trắng thì giữ mọi bài', () => {
    expect(matchesArticleQuery(uploaded, '   ')).toBe(true);
  });

  it('không khớp gì thì loại bài', () => {
    expect(matchesArticleQuery(uploaded, 'khong-co-gi')).toBe(false);
  });
});
