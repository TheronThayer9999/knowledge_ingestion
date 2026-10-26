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

// budget là số đo đã được chấp nhận cho một hàm đang vượt ngân sách.
type budget struct {
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
var functionBudgetAllowlist = map[string]budget{}

// TestFunctionBudget quét mọi hàm non-test trong repo và chặn hàm quá to.
func TestFunctionBudget(t *testing.T) {
	root := filepath.Join("..", "..")
	seen := make(map[string]bool, len(functionBudgetAllowlist))

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		return checkFile(t, root, path, seen)
	})
	if err != nil {
		t.Fatalf("duyệt cây mã: %v", err)
	}
	for key := range functionBudgetAllowlist {
		if !seen[key] {
			t.Errorf("allowlist còn %q nhưng hàm không còn vượt ngân sách (hoặc đã đổi tên) — xoá entry", key)
		}
	}
}

// checkFile parse 1 file rồi so từng hàm với ngân sách.
func checkFile(t *testing.T, root, path string, seen map[string]bool) error {
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

		allowed, listed := functionBudgetAllowlist[key]
		where := fmt.Sprintf("%s:%d", rel, fset.Position(fn.Pos()).Line)
		switch {
		case !listed:
			t.Errorf("%s %s: %d dòng, %d điểm quyết định (ngân sách %d/%d) — tách stage rồi mới thêm allowlist nếu thật sự cần",
				where, funcName(fn), lines, cx, maxFuncLines, maxFuncCX)
		case lines > allowed.Lines || cx > allowed.CX:
			t.Errorf("%s %s: %d dòng, %d điểm — vượt mức đã ghi trong allowlist (%d/%d), hàm đang phình thêm",
				where, funcName(fn), lines, cx, allowed.Lines, allowed.CX)
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
	for k := range functionBudgetAllowlist {
		keys = append(keys, k)
	}
	if !slices.IsSorted(keys) {
		t.Fatalf("allowlist phải sắp theo key: %v", keys)
	}
}
