package eino

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"

	"knowledge_ingestion/src/agent/retrieval"
	"knowledge_ingestion/src/domain"
)

// PHẦN LLM THẬT SỰ NHÌN THẤY về một tool là ToolInfo: name + desc + schema args.
// prompts/chat_system.md mục 11 cố ý KHÔNG chép tên tool ("danh sách tham số nằm
// ở định nghĩa tool đi kèm, nguồn thật duy nhất"), nên mô tả trong code là kênh
// duy nhất dạy model cách dùng tool sheet. Test này khoá đúng kênh đó: mất desc
// hoặc thiếu mô tả tham số thì model vẫn "thấy" tool nhưng không biết gọi thế nào.
//
// Đổi mô tả tool thì sửa cả test — cố ý, vì mô tả là hợp đồng với LLM.

// exposedTool là ToolInfo sau khi đi qua JSON (đúng dạng model nhận): schema nằm
// ở json_schema, mỗi tham số có description riêng.
type exposedTool struct {
	Name       string `json:"name"`
	Desc       string `json:"desc"`
	JSONSchema *struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	} `json:"json_schema"`
}

// infoOf lấy ToolInfo rồi marshal/unmarshal lại — không phụ thuộc field nội bộ
// của eino, chỉ phụ thuộc JSON mà model thật nhận được.
func infoOf(t *testing.T, tool einotool.BaseTool) exposedTool {
	t.Helper()
	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal ToolInfo: %v", err)
	}
	var got exposedTool
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal ToolInfo %s: %v", raw, err)
	}
	return got
}

// TestSheetToolsExposeTheirContract kiểm tra đúng 3 thứ model cần để tự gọi:
// tên tool, mô tả nói KHI NÀO dùng + trỏ sang tool còn lại (2 tool đi theo cặp),
// và từng tham số có mô tả (không có thì model bỏ trống article_id rồi lỗi).
func TestSheetToolsExposeTheirContract(t *testing.T) {
	listTool, err := NewListSheetsTool(&fakeIssueArticles{}, &fakeTextPages{})
	if err != nil {
		t.Fatalf("NewListSheetsTool: %v", err)
	}
	readTool, err := NewReadSheetTool(&fakeIssueArticles{}, &fakeTextPages{})
	if err != nil {
		t.Fatalf("NewReadSheetTool: %v", err)
	}
	list, read := infoOf(t, listTool), infoOf(t, readTool)

	cases := []struct {
		tool        exposedTool
		name        string
		mustMention []string
		args        []string
	}{
		{
			tool: list, name: ListSheetsName,
			mustMention: []string{"KHI NÀO GỌI", "KHI NÀO KHÔNG GỌI", "CÁCH DÙNG", "VÍ DỤ", "Excel", ReadSheetName},
			args:        []string{"article_id"},
		},
		{
			tool: read, name: ReadSheetName,
			mustMention: []string{"KHI NÀO GỌI", "KHI NÀO KHÔNG GỌI", "CÁCH DÙNG", "VÍ DỤ", "Excel", ListSheetsName},
			args:        []string{"article_id", "sheet", "from_row", "max_rows"},
		},
	}
	for _, c := range cases {
		if c.tool.Name != c.name {
			t.Fatalf("%s: name LLM thấy là %q", c.name, c.tool.Name)
		}
		if len(c.tool.Desc) < 100 {
			t.Fatalf("%s: desc quá ngắn để model quyết định gọi (%d ký tự)", c.name, len(c.tool.Desc))
		}
		for _, m := range c.mustMention {
			if !strings.Contains(c.tool.Desc, m) {
				t.Fatalf("%s: desc thiếu %q — model không biết khi nào gọi / gọi tiếp tool nào", c.name, m)
			}
		}
		if c.tool.JSONSchema == nil {
			t.Fatalf("%s: không có schema args", c.name)
		}
		required := strings.Join(c.tool.JSONSchema.Required, ",")
		if !strings.Contains(required, "article_id") {
			t.Fatalf("%s: article_id phải required, got %v", c.name, c.tool.JSONSchema.Required)
		}
		for _, arg := range c.args {
			prop, ok := c.tool.JSONSchema.Properties[arg]
			if !ok {
				t.Fatalf("%s: schema thiếu tham số %q (model không truyền được)", c.name, arg)
			}
			if strings.TrimSpace(prop.Description) == "" {
				t.Fatalf("%s: tham số %q không có description — model phải đoán giá trị", c.name, arg)
			}
		}
	}
}

// TestReadSheetTellsLlmHowToPage khoá đường đọc tiếp: sheet dài phải đọc nhiều
// lượt, nên mô tả PHẢI nhắc mốc next_from_row ở cả desc lẫn tham số from_row —
// thiếu thì model đọc trang đầu rồi kết luận "bảng chỉ có chừng này".
func TestReadSheetTellsLlmHowToPage(t *testing.T) {
	tool, err := NewReadSheetTool(&fakeIssueArticles{}, &fakeTextPages{})
	if err != nil {
		t.Fatalf("NewReadSheetTool: %v", err)
	}
	read := infoOf(t, tool)
	if !strings.Contains(read.Desc, "next_from_row") {
		t.Fatalf("desc read_sheet phải nhắc next_from_row để model đọc tiếp: %q", read.Desc)
	}
	if d := read.JSONSchema.Properties["from_row"].Description; !strings.Contains(d, "next_from_row") {
		t.Fatalf("mô tả from_row phải chỉ cách đọc tiếp bằng next_from_row: %q", d)
	}
	if d := read.JSONSchema.Properties["max_rows"].Description; !strings.Contains(d, "300") {
		t.Fatalf("mô tả max_rows phải nêu trần 300 để model không xin cả sheet: %q", d)
	}
}

// bareVectors/bareCategories nhúng interface nil: chỉ để dựng tool, test không
// gọi vào method nào (gọi sẽ panic — đúng ý, vì test này không kiểm hành vi).
type bareVectors struct{ domain.IVectorStore }

type bareCategories struct{ domain.ICategoryRepositoryImpl }

// toolArgStructs liệt kê MỌI struct args của tool trong package — thêm tool mới
// thì thêm vào đây để test dưới canh luôn kiểu tag.
var toolArgStructs = []any{
	KnowledgeSearchArgs{}, ListCategoriesArgs{}, ReadContextArgs{}, ArticleInfoArgs{},
	ListSectionsArgs{}, ReadArticleArgs{}, ListUnreadablePagesArgs{}, ListSheetsArgs{}, ReadSheetArgs{},
}

// TestToolArgTagsHaveNoCommaInDescription khoá một cái bẫy im lặng: eino dựng JSON
// schema args bằng cách tách tag `jsonschema` theo DẤU PHẨY, nên một dấu phẩy
// trong description cắt description tại đúng chỗ đó — model chỉ thấy nửa câu
// ("Số hàng mỗi lần đọc (mặc định 60") và hiểu sai tham số, không có lỗi nào
// được báo. Muốn ngắt câu trong description thì dùng ";" hoặc chấm, đừng dùng
// dấu phẩy (escape bằng "\\," cũng không qua được bước unquote của Go).
func TestToolArgTagsHaveNoCommaInDescription(t *testing.T) {
	for _, sample := range toolArgStructs {
		typ := reflect.TypeOf(sample)
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			tag, ok := f.Tag.Lookup("jsonschema")
			if !ok {
				continue
			}
			for _, part := range strings.Split(tag, ",") {
				part = strings.TrimSpace(part)
				if part == "" || part == "required" || part == "nullable" {
					continue
				}
				if !strings.Contains(part, "=") {
					t.Fatalf("%s.%s: mảnh tag %q không phải key=value — description có dấu phẩy chưa xử lý (dùng ';' thay vì ',')",
						typ.Name(), f.Name, part)
				}
			}
		}
	}
}

// TestDefaultToolsExposeAllNames khoá danh sách tool mà MỌI provider agent nhận
// từ NewDefaultTools → BuildProviderAgents. Thêm tool mới quên đăng ký, hoặc
// lỡ tay trả slice rỗng, thì test này gãy thay vì để model "không biết" tool tồn
// tại một cách âm thầm.
func TestDefaultToolsExposeAllNames(t *testing.T) {
	vectors := &bareVectors{}
	tools, err := NewDefaultTools(ToolDeps{
		Hybrid:     retrieval.NewHybridSearcher(nil, vectors, &fakeIssueArticles{}),
		Vectors:    vectors,
		Categories: &bareCategories{},
		Articles:   &fakeIssueArticles{},
		Pages:      &fakeIssuePages{},
		PageText:   &fakeTextPages{},
	})
	if err != nil {
		t.Fatalf("NewDefaultTools: %v", err)
	}

	seen := make(map[string]bool, len(tools))
	for _, tl := range tools {
		info, err := tl.Info(context.Background())
		if err != nil {
			t.Fatalf("Info: %v", err)
		}
		if strings.TrimSpace(info.Name) == "" {
			t.Fatal("có tool không có name — model không gọi được")
		}
		if strings.TrimSpace(info.Desc) == "" {
			t.Fatalf("tool %s không có desc — model không biết khi nào gọi", info.Name)
		}
		if seen[info.Name] {
			t.Fatalf("tool %s khai báo 2 lần — model thấy trùng", info.Name)
		}
		seen[info.Name] = true
	}
	for _, want := range []string{ListSheetsName, ReadSheetName} {
		if !seen[want] {
			t.Fatalf("thiếu tool %s trong danh sách model nhận", want)
		}
	}
	if len(tools) != 9 {
		t.Fatalf("số tool đổi (%d) — cập nhật prompt/test nếu cố ý thêm/bớt", len(tools))
	}
}
