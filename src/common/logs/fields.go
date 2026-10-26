package logs

// Chính sách định dạng field ÁP DỤNG CHO MỌI dòng log có cấu trúc trong dự án
// (Infow/Warnw/Error cùng các bản *Skip). Đặt ở đây thay vì sửa từng call site:
// gọi 100 nơi sắp xếp tay thì liệu có hết, và log phát ra từ thư viện (logger
// gorm, transport LLM, access middleware) cũng đi qua đây nên tự động cùng
// khuôn — không thể "quên" một gói logs.Infow ra ngoài lệ.
//
// Mục tiêu là đọc được: quét mắt nhanh qua cột đầu dòng, so sánh hai dòng cùng
// kind, và không bị một giá trị dài (SQL, câu hỏi user, URL ký) tràn đầy màn
// hình.

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"
)

// maxLogValueRunes trần độ dài giá trị kiểu chuỗi. 512 đủ cho tên tài liệu,
// đường dẫn, message của provider, và chặn đúng chỗ những SQL/nội dung dung
// lượng lớn trước khi chúng choáng hết dòng log. Đếm RUNE chứ không đếm BYTE —
// tiếng Việt có dấu 3 byte/ký tự, cắt theo byte sẽ cắt giữa chữ và in ra ký tự
// rác.
const maxLogValueRunes = 512

// hiddenQueryNote nói rõ phần đã bị ẩn, để người đọc không tưởng URL thiếu
// query là lỗi log. Query của link ký (presigned) chứa credential tạm; query
// của link người dùng dán vào còn có thể chứa thông tin tế nhị — không in ra
// log.
const hiddenQueryNote = " [đã ẩn query]"

const (
	keyTraceID = "trace_id"
	keyError   = "error"
	keyID      = "id"
	idSuffix   = "_id"
)

// fieldRank qui định tầng thứ tự (nhỏ đứng trước). Chỉ 4 tầng, không hard-code
// danh sách 30 tên field — thêm field mới thì log tự động vào đúng chỗ, không
// phải quay lại file này.
//
//	0 trace_id : khóa nối các dòng của cùng một request/job, phải đứng đầu
//	1 *_id     : nhận diện "cái gì/ai" (id, article_id, user_id…)
//	2 còn lại  : thông thường (model, status, size, sql, message_chars…)
//	3 error    : luôn cuối — giá trị dài nhất và hay đổi nhất, nằm cuối thì
//	           phần đầu dòng log vẫn đọc được dù nó có bị cắt
func fieldRank(key string) int {
	switch key {
	case keyTraceID:
		return 0
	case keyError:
		return 3
	case keyID:
		return 1
	}
	if strings.HasSuffix(key, idSuffix) {
		return 1
	}
	return 2
}

// logField là một cặp key/value tách từ kv.
type logField struct {
	key   string
	value any
}

// normalizeFields sắp lại thứ tự field theo tầng (trong cùng tầng thì A→Z để
// kết quả tất định), đồng thời cắt giá trị chuỗi dài và ẩn query của URL.
//
// Được phép "bỏ qua" khi kv lẻ số phần tử hoặc có key không phải chuỗi: zap có
// cách in riêng cho trường hợp đó (!BADKEY/MISSING), đoạn log của thư viện như
// gorm logger nhi khi truyền giá trị tràn vào ô của key — đoán nhầm thành tên
// field mới còn hại hơn là giữ nguyên.
func normalizeFields(kv []any) []any {
	if len(kv) < 2 || len(kv)%2 != 0 {
		return kv
	}
	fields := make([]logField, 0, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok {
			return kv
		}
		fields = append(fields, logField{key: key, value: sanitizeValue(kv[i+1])})
	}
	sort.SliceStable(fields, func(i, j int) bool {
		ri, rj := fieldRank(fields[i].key), fieldRank(fields[j].key)
		if ri != rj {
			return ri < rj
		}
		return fields[i].key < fields[j].key
	})
	out := make([]any, 0, len(kv))
	for _, f := range fields {
		out = append(out, f.key, f.value)
	}
	return out
}

// sanitizeValue chỉ đụng những giá trị dạng chuỗi. error để nguyên để zap vẫn
// encode đúng ErrorType (log JSON giữ hình dạng cũ); message của provider đã
// được tóm ở tầng nghiệp vụ nên không cắt thêm.
func sanitizeValue(v any) any {
	switch val := v.(type) {
	case string:
		return sanitizeString(val)
	case []byte:
		return sanitizeString(string(val))
	default:
		return v
	}
}

func sanitizeString(s string) string {
	return truncateRunes(hideURLQuery(s))
}

// hideURLQuery rút gọn URL tuyệt đối có query/fragment còn scheme://host/path.
// Chỉ nhận khi chuỗi bắt đầu bằng http(s):// để không chạm vào câu SQL hay văn
// bản tình cờ có chữ "http".
func hideURLQuery(s string) string {
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return s
	}
	u, err := url.Parse(s)
	if err != nil || (u.RawQuery == "" && u.Fragment == "") {
		return s
	}
	u.RawQuery, u.ForceQuery, u.Fragment, u.RawFragment = "", false, "", ""
	return u.String() + hiddenQueryNote
}

// truncateRunes cắt chuỗi dài quá trần, giữ lại đầu câu và nói rõ còn bao nhiêu
// ký tự nữa để người đọc biết dòng log bị gọn chứ không bị mất.
func truncateRunes(s string) string {
	n := utf8.RuneCountInString(s)
	if n <= maxLogValueRunes {
		return s
	}
	head := []rune(s)[:maxLogValueRunes]
	return fmt.Sprintf("%s…[+%d ký tự]", string(head), n-maxLogValueRunes)
}
