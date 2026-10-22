package services

import (
	"context"
	"testing"

	"knowledge_ingestion/src/controller/dtos"
	"knowledge_ingestion/src/domain"
)

// ---- stubs ----

type catStubRepo struct {
	exists map[int64]bool
	byID   map[int64]*domain.Category
	deleted []int64
}

func (r *catStubRepo) Create(ctx context.Context, t *domain.Category) error {
	if t.ID == 0 {
		t.ID = 1
	}
	return nil
}
func (r *catStubRepo) Update(ctx context.Context, t *domain.Category) error { return nil }
func (r *catStubRepo) Delete(ctx context.Context, t *domain.Category) error  { return nil }
func (r *catStubRepo) DeleteByIDs(ctx context.Context, ids []int64) error {
	r.deleted = append([]int64{}, ids...)
	return nil
}
func (r *catStubRepo) GetById(ctx context.Context, id int64, userId int64) (*domain.Category, error) {
	c, ok := r.byID[id]
	if !ok || c.UserID != userId {
		return nil, domain.ErrNotFound
	}
	return c, nil
}
func (r *catStubRepo) GetByName(ctx context.Context, name string, userId int64) (*domain.Category, error) {
	return nil, domain.ErrNotFound
}
func (r *catStubRepo) GetAll(ctx context.Context, userId int64) ([]*domain.Category, error) {
	return nil, nil
}
func (r *catStubRepo) Exists(ctx context.Context, id int64, userId int64) (bool, error) {
	if r.exists != nil {
		return r.exists[id], nil
	}
	_, err := r.GetById(ctx, id, userId)
	return err == nil, nil
}

type catStubClosure struct {
	descendants map[int64][]*domain.CategoryWithDepth
	ancestors   map[int64][]*domain.CategoryWithDepth
	isAncestor  map[string]bool
	removed     [][]int64
	moved       []struct {
		id   int64
		pid  *int64
	}
	addedTree []struct {
		id  int64
		pid *int64
	}
}

func (r *catStubClosure) AddToTree(ctx context.Context, categoryID int64, parentID *int64) error {
	r.addedTree = append(r.addedTree, struct {
		id  int64
		pid *int64
	}{categoryID, parentID})
	return nil
}
func (r *catStubClosure) Move(ctx context.Context, categoryID int64, newParentID *int64) error {
	r.moved = append(r.moved, struct {
		id  int64
		pid *int64
	}{categoryID, newParentID})
	return nil
}
func (r *catStubClosure) RemoveSubtree(ctx context.Context, categoryIDs []int64) error {
	r.removed = append(r.removed, append([]int64{}, categoryIDs...))
	return nil
}
func (r *catStubClosure) IsAncestor(ctx context.Context, ancestorID, descendantID int64) (bool, error) {
	if r.isAncestor != nil {
		return r.isAncestor[strconv2(ancestorID, descendantID)], nil
	}
	return false, nil
}
func (r *catStubClosure) GetByOwner(ctx context.Context, userID int64) ([]*domain.CategoryClosure, error) {
	return nil, nil
}
func (r *catStubClosure) GetDescendants(ctx context.Context, categoryID int64) ([]*domain.CategoryWithDepth, error) {
	if r.descendants != nil {
		return r.descendants[categoryID], nil
	}
	// default: self only
	return []*domain.CategoryWithDepth{{Category: domain.Category{BaseModel: domain.BaseModel{ID: categoryID}}, Depth: 0}}, nil
}
func (r *catStubClosure) GetAncestors(ctx context.Context, categoryID int64) ([]*domain.CategoryWithDepth, error) {
	if r.ancestors != nil {
		return r.ancestors[categoryID], nil
	}
	return []*domain.CategoryWithDepth{{Category: domain.Category{BaseModel: domain.BaseModel{ID: categoryID}}, Depth: 0}}, nil
}

func strconv2(a, b int64) string {
	return itoa(a) + "," + itoa(b)
}
func itoa(n int64) string {
	// tiny helper to avoid importing strconv in test stub
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}

type catStubUoW struct{}

func (u *catStubUoW) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

type catStubCascade struct {
	deleted int64
	ids     []int64
}

func (c *catStubCascade) DeleteByCategoryIDs(ctx context.Context, ids []int64) (int64, error) {
	c.ids = append([]int64{}, ids...)
	c.deleted = int64(len(ids)) // pretend 1 article per folder for assertion? keep 0 to assert called
	return c.deleted, nil
}

func newCatSvc(repo *catStubRepo, closure *catStubClosure, cascade *catStubCascade) ICategoryService {
	return NewCategoryService(repo, &catStubUoW{}, closure, cascade)
}

// ---- tests ----

func TestCategoryCreateParentNotFound(t *testing.T) {
	repo := &catStubRepo{exists: map[int64]bool{5: false}}
	svc := newCatSvc(repo, &catStubClosure{}, &catStubCascade{})
	pid := int64(5)
	res := svc.Create(context.Background(), 10, &dtos.CreateCategoryRequest{Name: "child", ParentID: &pid})
	if res.Err == nil {
		t.Fatal("parent không tồn tại phải báo lỗi")
	}
}

func TestCategoryCreateRootSuccess(t *testing.T) {
	repo := &catStubRepo{}
	closure := &catStubClosure{}
	svc := newCatSvc(repo, closure, &catStubCascade{})
	res := svc.Create(context.Background(), 10, &dtos.CreateCategoryRequest{Name: "root"})
	if res.Err != nil {
		t.Fatalf("Create lỗi: %v", res.Err)
	}
	if len(closure.addedTree) != 1 || closure.addedTree[0].pid != nil {
		t.Errorf("root phải AddToTree với parent nil, got %+v", closure.addedTree)
	}
}

func TestCategoryCreateChildSuccess(t *testing.T) {
	pid := int64(7)
	repo := &catStubRepo{exists: map[int64]bool{7: true}}
	closure := &catStubClosure{}
	svc := newCatSvc(repo, closure, &catStubCascade{})
	res := svc.Create(context.Background(), 10, &dtos.CreateCategoryRequest{Name: "child", ParentID: &pid})
	if res.Err != nil {
		t.Fatalf("Create lỗi: %v", res.Err)
	}
	if len(closure.addedTree) != 1 || closure.addedTree[0].pid == nil || *closure.addedTree[0].pid != 7 {
		t.Errorf("child phải AddToTree với parent 7, got %+v", closure.addedTree)
	}
}

func TestCategoryUpdateCycleDetection(t *testing.T) {
	repo := &catStubRepo{byID: map[int64]*domain.Category{1: {BaseModel: domain.BaseModel{ID: 1}, UserID: 10}}}
	closure := &catStubClosure{isAncestor: map[string]bool{strconv2(1, 2): true}}
	svc := newCatSvc(repo, closure, &catStubCascade{})
	pid := int64(2)
	res := svc.Update(context.Background(), 1, 10, &dtos.UpdateCategoryRequest{ParentID: &pid})
	if res.Err == nil {
		t.Fatal("chuyển vào nhánh con phải báo lỗi cycle")
	}
}

func TestCategoryUpdateSelfParent(t *testing.T) {
	repo := &catStubRepo{byID: map[int64]*domain.Category{1: {BaseModel: domain.BaseModel{ID: 1}, UserID: 10}}}
	svc := newCatSvc(repo, &catStubClosure{}, &catStubCascade{})
	pid := int64(1)
	res := svc.Update(context.Background(), 1, 10, &dtos.UpdateCategoryRequest{ParentID: &pid})
	if res.Err == nil {
		t.Fatal("tự làm cha phải báo lỗi")
	}
}

func TestCategoryUpdateParentNotFound(t *testing.T) {
	repo := &catStubRepo{byID: map[int64]*domain.Category{1: {BaseModel: domain.BaseModel{ID: 1}, UserID: 10}}}
	svc := newCatSvc(repo, &catStubClosure{}, &catStubCascade{})
	pid := int64(99)
	res := svc.Update(context.Background(), 1, 10, &dtos.UpdateCategoryRequest{ParentID: &pid})
	if res.Err == nil {
		t.Fatal("cha không tồn tại phải báo lỗi")
	}
}

func TestCategoryDeleteCascade(t *testing.T) {
	repo := &catStubRepo{byID: map[int64]*domain.Category{1: {BaseModel: domain.BaseModel{ID: 1}, UserID: 10}}}
	closure := &catStubClosure{descendants: map[int64][]*domain.CategoryWithDepth{
		1: {{Category: domain.Category{BaseModel: domain.BaseModel{ID: 2}}, Depth: 1}},
	}}
	cascade := &catStubCascade{}
	svc := newCatSvc(repo, closure, cascade)
	res := svc.Delete(context.Background(), 1, 10)
	if res.Err != nil {
		t.Fatalf("Delete lỗi: %v", res.Err)
	}
	// folderIDs phải gồm 1 và 2
	if len(repo.deleted) == 0 {
		t.Fatal("DeleteByIDs không được gọi")
	}
	got := map[int64]bool{}
	for _, id := range repo.deleted {
		got[id] = true
	}
	if !got[1] || !got[2] {
		t.Errorf("folderIDs thiếu, got %v", repo.deleted)
	}
	if len(cascade.ids) == 0 {
		t.Fatal("ArticleCategoryCascade không được gọi")
	}
	if len(closure.removed) == 0 {
		t.Fatal("RemoveSubtree không được gọi")
	}
}

func TestCategoryCrossUserIsolation(t *testing.T) {
	repo := &catStubRepo{byID: map[int64]*domain.Category{1: {BaseModel: domain.BaseModel{ID: 1}, UserID: 10}}}
	svc := newCatSvc(repo, &catStubClosure{}, &catStubCascade{})
	// user 33 không sở hữu category 1
	res := svc.GetById(context.Background(), 1, 33)
	if res.Err == nil {
		t.Fatal("user khác phải 404")
	}
	res2 := svc.Delete(context.Background(), 1, 33)
	if res2.Err == nil {
		t.Fatal("user khác xóa phải 404")
	}
}