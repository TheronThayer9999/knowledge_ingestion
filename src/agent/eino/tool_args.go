package eino

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// flexInt64/flexInt là kiểu số cho args tool, chấp nhận cả number, chuỗi số
// và giá trị vô nghĩa mà model free hay sinh ra ("None", tên file,
// "10" dạng string). Unmarshal cứng int64 làm ToolNode abort cả run thành
// 500 (TC26/TC33/PH29 trong full-case OpenRouter) — ở đây ép mọi thứ về số,
// không parse được thì về 0 để validation trong tool trả kết quả rỗng/thân
// thiện, LLM tự sửa args mà không sập run.
type flexInt64 int64

type flexInt int

func parseFlexInt(b []byte) int64 {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return 0
	}
	if b[0] == '"' {
		var s string
		if json.Unmarshal(b, &s) != nil {
			return 0
		}
		s = strings.TrimSpace(s)
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		}
		// "10.0" dạng string vẫn thành số, "None"/tên file về 0.
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return int64(f)
		}
		return 0
	}
	var f float64
	if json.Unmarshal(b, &f) != nil {
		return 0
	}
	return int64(f)
}

func (f *flexInt64) UnmarshalJSON(b []byte) error {
	*f = flexInt64(parseFlexInt(b))
	return nil
}

func (f *flexInt) UnmarshalJSON(b []byte) error {
	*f = flexInt(parseFlexInt(b))
	return nil
}
