//go:build e2e

package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
)

func e2eBase() string {
	if v := os.Getenv("E2E_BASE_URL"); v != "" {
		return v
	}
	return "http://localhost:9001"
}

func e2eDo(t *testing.T, method, path string, body any, token string) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, e2eBase()+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestE2ECategoryCascadeDelete(t *testing.T) {
	// 1. Tạo user tạm
	username := fmt.Sprintf("e2e_%d", os.Getpid())
	status, _ := e2eDo(t, "POST", "/api/v1/users", map[string]any{
		"username": username, "password": "e2ePass123", "email": username + "@example.com",
	}, "")
	if status != 200 {
		t.Fatalf("create user: %d", status)
	}
	// 2. Login
	status, out := e2eDo(t, "POST", "/api/v1/auth/login", map[string]any{
		"username": username, "password": "e2ePass123",
	}, "")
	if status != 200 {
		t.Fatalf("login: %d", status)
	}
	token := out["data"].(map[string]any)["token"].(string)

	// 3. Tạo parent + child
	status, out = e2eDo(t, "POST", "/api/v1/categories", map[string]any{"name": "parent"}, token)
	if status != 200 {
		t.Fatalf("create parent: %d", status)
	}
	parentID := int64(out["data"].(map[string]any)["id"].(float64))

	status, out = e2eDo(t, "POST", "/api/v1/categories", map[string]any{"name": "child", "parent_id": parentID}, token)
	if status != 200 {
		t.Fatalf("create child: %d", status)
	}
	childID := int64(out["data"].(map[string]any)["id"].(float64))

	// 4. Xóa parent
	status, _ = e2eDo(t, "DELETE", fmt.Sprintf("/api/v1/categories/%d", parentID), nil, token)
	if status != 200 {
		t.Fatalf("delete parent: %d", status)
	}

	// 5. Child phải 404
	status, _ = e2eDo(t, "GET", fmt.Sprintf("/api/v1/categories/%d", childID), nil, token)
	if status != 404 {
		t.Fatalf("child phải 404 sau cascade delete, got %d", status)
	}
}