package quality

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Ngân sách hàm của repo. Lý do chọn số: xem plans/2026-10-27-dot-9-*.md.
//
//	8–45 dòng  : kích thước đích (vừa một màn hình, vừa một tầng if)
//	60 dòng    : trần cứng — quá số này gần như chắc chắn trộn ≥ 2 tầng trừu tượng
//	12 điểm    : điểm quyết định (if/for/case/&&/||) — quá số này không còn đọc nổi
//
// Test này KHÔNG có trần dưới: hàm 3 dòng là đúng khi nó là khái niệm, là
// constructor hay để thoả interface, và sai khi chỉ chuyển tiếp một lệnh. Máy
// không phân biệt được hai ca đó (cùng hình dạng AST), nên phần "hàm quá nhỏ"
// chỉ nằm trong review checklist ở AGENTS.md, không thành test.
const (
	maxFuncLines = 60
	maxFuncCX    = 12
)

// allowEntry là một dòng nợ kỹ thuật: hàm đang vượt ngân sách + mức đã đo.
// Dùng slice thay map để thứ tự còn quan sát được (map của Go không giữ thứ tự
// nên không thể canh "danh sách phải sắp theo key").
type allowEntry struct {
	Key   string
	Lines int
	CX    int
}

// functionBudgetAllowlist là nợ kỹ thuật ĐANG TRẢ DẦN: mỗi entry là một hàm
// vượt ngân sách đã đo. Hai luật do test này cưỡng chế:
//   - hàm trong danh sách KHÔNG được phình thêm (Lines/CX vượt số đã ghi);
//   - hàm đã xuống dưới ngân sách thì PHẢI xoá entry (entry cũ = test đỏ),
//     nên danh sách chỉ ngắn đi, không dài ra.
//
// Key = "đường/dẫn/file.go::TênHàm", method ghi dạng "(*Type).Method".
var functionBudgetAllowlist = []allowEntry{
	{"cmd/examples/main.go::main", 67, 4},
	{"cmd/examples/main.go::runAgent", 71, 8},
	{"src/agent/eino/agent.go::(*ChatAgent).Run", 67, 17},
	{"src/agent/eino/knowledge_search.go::NewKnowledgeSearchTool", 78, 14},
	{"src/agent/eino/list_unreadable_pages.go::NewListUnreadablePagesTool", 76, 15},
	{"src/agent/eino/model.go::(*retryTransport).RoundTrip", 55, 17},
	{"src/agent/eino/read_article.go::NewReadArticleTool", 96, 19},
	{"src/agent/eino/read_context.go::NewArticleInfoTool", 73, 17},
	{"src/agent/eino/read_context.go::NewReadContextTool", 68, 13},
	{"src/agent/eino/read_sheet.go::NewReadSheetTool", 83, 18},
	{"src/agent/retrieval/hybrid_search.go::(*HybridSearcher).SearchHybrid", 67, 15},
	{"src/common/chunker/section.go::findHeadings", 37, 20},
	{"src/common/chunker/template.go::DetectType", 16, 15},
	{"src/common/extractor/compare.go::compareTokens", 37, 17},
	{"src/common/extractor/despace.go::joinLine", 67, 16},
	{"src/common/extractor/docx.go::extractDocx", 166, 48},
	{"src/common/extractor/pdf.go::extractPDFPages", 89, 14},
	{"src/common/extractor/pptx.go::pptxParagraphs", 40, 13},
	{"src/common/extractor/xlsx.go::xlsxRows", 112, 30},
	{"src/common/extractor/xlsx.go::xlsxSharedStrings", 48, 13},
	{"src/config/config.go::Load", 223, 67},
	// Bảng route thuần (1 điểm quyết định): mỗi endpoint thêm là mỗi dòng dài
	// ra — nợ này lớn theo API, không phải theo độ phức tạp, nên cho tăng
	// thay vì cắt hàm ra từng mảnh vô nghĩa.
	{"src/controller/routers/router.go::NewRouter", 89, 1},
	{"src/controller/services/article_admin_service.go::(*articleAdminService).QueueStatus", 88, 9},
	{"src/controller/services/article_purge_service.go::(*articlePurgeService).PurgeDeleted", 86, 12},
	{"src/controller/services/category_service.go::(*categoryService).Update", 57, 19},
	{"src/controller/services/user_service.go::(*userService).Refresh", 67, 14},
	{"src/infrastructure/postgres/migrate.go::migrate", 102, 15},
	{"src/infrastructure/postgres/migrate.go::migrateCategoryClosure", 61, 9},
	{"src/infrastructure/qdrant/connection.go::(*connection).ensureCollection", 68, 17},
	{"src/infrastructure/vlm/ollama/client.go::(*Reader).chat", 63, 13},
	{"src/loader/worker.go::LoadWorkerInfra", 142, 15},
}

// allowlistByKey dựng bảng tra một lần cho mỗi lượt chạy test.
func allowlistByKey() map[string]allowEntry {
	out := make(map[string]allowEntry, len(functionBudgetAllowlist))
	for _, e := range functionBudgetAllowlist {
		out[e.Key] = e
	}
	return out
}

// TestFunctionBudget quét mọi hàm non-test trong repo và chặn hàm quá to.
func TestFunctionBudget(t *testing.T) {
	root := filepath.Join("..", "..")
	allowed := allowlistByKey()
	seen := make(map[string]bool, len(allowed))

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Root là "../.." nên d.Name() == "..": nếu skip theo tên thì walk
			// bỏ luôn cả gốc và test xanh rỗng.
			if path != root && skipDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		return checkFile(t, root, path, allowed, seen)
	})
	if err != nil {
		t.Fatalf("duyệt cây mã: %v", err)
	}
	for _, e := range functionBudgetAllowlist {
		if !seen[e.Key] {
			t.Errorf("allowlist còn %q nhưng hàm không còn vượt ngân sách (hoặc đã đổi tên) — xoá entry", e.Key)
		}
	}
}

// checkFile parse 1 file rồi so từng hàm với ngân sách.
func checkFile(t *testing.T, root, path string, allowed map[string]allowEntry, seen map[string]bool) error {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		lines := fset.Position(fn.End()).Line - fset.Position(fn.Pos()).Line + 1
		cx := decisionPoints(fn)
		if lines <= maxFuncLines && cx <= maxFuncCX {
			continue
		}
		key := rel + "::" + funcName(fn)
		seen[key] = true

		where := fmt.Sprintf("%s:%d", rel, fset.Position(fn.Pos()).Line)
		all, listed := allowed[key]
		switch {
		case !listed:
			t.Errorf("%s %s: %d dòng, %d điểm quyết định (ngân sách %d/%d) — tách stage rồi mới thêm allowlist nếu thật sự cần",
				where, funcName(fn), lines, cx, maxFuncLines, maxFuncCX)
		case lines > all.Lines || cx > all.CX:
			t.Errorf("%s %s: %d dòng, %d điểm — vượt mức đã ghi trong allowlist (%d/%d), hàm đang phình thêm",
				where, funcName(fn), lines, cx, all.Lines, all.CX)
		}
	}
	return nil
}

// skipDir bỏ những cây không phải mã Go của mình (và mọi thư mục ẩn như .git,
// .codegraph) — quét vào đó chỉ tốn thời gian và báo sai.
func skipDir(name string) bool {
	switch name {
	case "node_modules", "vendor", "docs", "dist", "knowledge_ingestion_fe":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// decisionPoints đếm điểm quyết định — thước đo "hàm dễ đọc không" rẻ hơn
// cyclomatic đầy đủ và đủ bắt các hàm trộn nhiều nhánh.
func decisionPoints(fn *ast.FuncDecl) int {
	n := 0
	ast.Inspect(fn, func(node ast.Node) bool {
		switch x := node.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.CaseClause, *ast.CommClause:
			n++
		case *ast.BinaryExpr:
			if x.Op == token.LAND || x.Op == token.LOR {
				n++
			}
		}
		return true
	})
	return n
}

// funcName trả tên hàm kèm receiver để key allowlist không đụng nhau giữa các
// type khác nhau có cùng tên method.
func funcName(fn *ast.FuncDecl) string {
	name := fn.Name.Name
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return name
	}
	if t := receiverType(fn.Recv.List[0].Type); t != "" {
		return "(*" + t + ")." + name
	}
	return name
}

func receiverType(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.StarExpr:
		return receiverType(v.X)
	case *ast.IndexExpr: // receiver generic: *Registry[T]
		return receiverType(v.X)
	case *ast.Ident:
		return v.Name
	}
	return ""
}

// TestFunctionBudgetAllowlistSorted không bắt buộc về mặt kỹ thuật, nhưng giữ
// danh sách đọc được như một bảng nợ kỹ thuật (và tránh conflict khi hai người
// cùng xoá entry).
func TestFunctionBudgetAllowlistSorted(t *testing.T) {
	keys := make([]string, 0, len(functionBudgetAllowlist))
	for _, e := range functionBudgetAllowlist {
		keys = append(keys, e.Key)
	}
	if !slices.IsSorted(keys) {
		t.Fatalf("allowlist phải sắp theo key: %v", keys)
	}
	if slices.Contains(slices.Compact(slices.Clone(keys)), "") {
		t.Fatal("allowlist có key rỗng")
	}
	if len(slices.Compact(slices.Clone(keys))) != len(keys) {
		t.Fatal("allowlist có key trùng")
	}
}
