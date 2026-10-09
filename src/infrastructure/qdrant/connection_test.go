package qdrant

// Test REST client Qdrant bằng httptest — không cần Qdrant thật. Assert
// ensure/upsert/delete bắn đúng path + body + header api-key.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"knowledge_ingestion/src/common/constants"
	"knowledge_ingestion/src/domain"
)

type recordedReq struct {
	method string
	path   string
	apiKey string
	body   map[string]any
}

func newTestServer(t *testing.T, collectionExists bool, existingDim int, recs *[]recordedReq) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Body != nil {
			data, _ := io.ReadAll(r.Body)
			if len(data) > 0 {
				_ = json.Unmarshal(data, &body)
			}
		}
		*recs = append(*recs, recordedReq{
			method: r.Method, path: r.URL.RequestURI(),
			apiKey: r.Header.Get("api-key"), body: body,
		})
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/collections/"):
			if !collectionExists {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{
				"config": map[string]any{"params": map[string]any{
					"vectors": map[string]any{"size": existingDim, "distance": "Cosine"},
				}},
			}})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/collections/"):
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
}

// Collection chưa có → tạo mới với đúng dim + gửi api-key.
func TestEnsureCollection_CreatesWhenMissing(t *testing.T) {
	var recs []recordedReq
	srv := newTestServer(t, false, 0, &recs)
	defer srv.Close()

	c := newClient(srv.URL, "secret-token", 1024)
	if err := c.ensureCollection(context.Background()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	var put, index bool
	for _, r := range recs {
		if r.apiKey != "secret-token" {
			t.Fatalf("thiếu header api-key: %+v", r)
		}
		if r.method == http.MethodPut && strings.HasSuffix(r.path, "/collections/"+constants.QDRANT_COLLECTION) {
			put = true
			vecs, _ := r.body["vectors"].(map[string]any)
			if vecs["distance"] != "Cosine" || vecs["size"] != float64(1024) {
				t.Fatalf("body tạo collection sai: %v", r.body)
			}
		}
		if r.method == http.MethodPut && strings.HasSuffix(r.path, "/index") {
			index = true
		}
	}
	if !put || !index {
		t.Fatalf("thiếu PUT create hoặc PUT index: %+v", recs)
	}
}

// Collection có sẵn sai dim → lỗi fail-fast, không upsert mù.
func TestEnsureCollection_WrongDimFails(t *testing.T) {
	var recs []recordedReq
	srv := newTestServer(t, true, 768, &recs)
	defer srv.Close()

	c := newClient(srv.URL, "", 1024)
	if err := c.ensureCollection(context.Background()); err == nil {
		t.Fatal("sai dim phải lỗi")
	}
}

// Upsert đúng shape: id uuid, vector, payload đủ 5 field.
func TestUpsertPoints_Shape(t *testing.T) {
	var recs []recordedReq
	srv := newTestServer(t, true, 4, &recs)
	defer srv.Close()

	c := newClient(srv.URL, "", 4)
	err := c.UpsertPoints(context.Background(), []*domain.VectorPoint{{
		ID: "550e8400-e29b-41d4-a716-446655440000", Vector: []float32{0.1, 0.2, 0.3, 0.4},
		ArticleID: 5, UserID: 7, CategoryID: 3, ChunkIndex: 1, Text: "đoạn",
	}})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	var found bool
	for _, r := range recs {
		if r.method != http.MethodPut || !strings.Contains(r.path, "/points") {
			continue
		}
		if !strings.Contains(r.path, "wait=true") {
			t.Fatalf("upsert phải wait=true: %s", r.path)
		}
		found = true
		pts, _ := r.body["points"].([]any)
		if len(pts) != 1 {
			t.Fatalf("points = %v", r.body)
		}
		p, _ := pts[0].(map[string]any)
		if p["id"] != "550e8400-e29b-41d4-a716-446655440000" {
			t.Fatalf("id sai: %v", p)
		}
		payload, _ := p["payload"].(map[string]any)
		for _, k := range []string{"article_id", "user_id", "category_id", "chunk_index", "page_num", "text"} {
			if _, ok := payload[k]; !ok {
				t.Fatalf("payload thiếu %s: %v", k, payload)
			}
		}
	}
	if !found {
		t.Fatal("không thấy request upsert")
	}
}

// Vector sai dim thì lỗi ngay, không bắn request.
func TestUpsertPoints_DimMismatch(t *testing.T) {
	var recs []recordedReq
	srv := newTestServer(t, true, 4, &recs)
	defer srv.Close()

	c := newClient(srv.URL, "", 4)
	err := c.UpsertPoints(context.Background(), []*domain.VectorPoint{
		{ID: "x", Vector: []float32{0.1}},
	})
	if err == nil {
		t.Fatal("sai dim phải lỗi")
	}
	for _, r := range recs {
		if strings.Contains(r.path, "/points") {
			t.Fatalf("sai dim mà vẫn bắn request: %+v", r)
		}
	}
}

// Upsert rỗng → no-op, không bắn request.
func TestUpsertPoints_EmptyNoop(t *testing.T) {
	var recs []recordedReq
	srv := newTestServer(t, true, 4, &recs)
	defer srv.Close()

	c := newClient(srv.URL, "", 4)
	if err := c.UpsertPoints(context.Background(), nil); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("không được bắn request, got %+v", recs)
	}
}

// Count đúng shape: filter article_id + decode result.count.
func TestCountByArticle_Shape(t *testing.T) {
	var recs []recordedReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Body != nil {
			data, _ := io.ReadAll(r.Body)
			if len(data) > 0 {
				_ = json.Unmarshal(data, &body)
			}
		}
		recs = append(recs, recordedReq{
			method: r.Method, path: r.URL.RequestURI(),
			apiKey: r.Header.Get("api-key"), body: body,
		})
		_, _ = w.Write([]byte(`{"result":{"count":7},"status":"ok"}`))
	}))
	defer srv.Close()

	c := newClient(srv.URL, "", 4)
	n, err := c.CountByArticle(context.Background(), 5)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 7 {
		t.Fatalf("count = %d, want 7", n)
	}
	if len(recs) != 1 || recs[0].method != http.MethodPost || !strings.Contains(recs[0].path, "/points/count") {
		t.Fatalf("request sai: %+v", recs)
	}
	filter, _ := recs[0].body["filter"].(map[string]any)
	must, _ := filter["must"].([]any)
	cond, _ := must[0].(map[string]any)
	if cond["key"] != "article_id" {
		t.Fatalf("filter sai key: %v", cond)
	}
}
func TestDeleteByArticle_Filter(t *testing.T) {
	var recs []recordedReq
	srv := newTestServer(t, true, 4, &recs)
	defer srv.Close()

	c := newClient(srv.URL, "", 4)
	if err := c.DeleteByArticle(context.Background(), 9); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	var found bool
	for _, r := range recs {
		if r.method != http.MethodPost || !strings.Contains(r.path, "/points/delete") {
			continue
		}
		found = true
		filter, _ := r.body["filter"].(map[string]any)
		must, _ := filter["must"].([]any)
		if len(must) != 1 {
			t.Fatalf("filter sai: %v", r.body)
		}
		cond, _ := must[0].(map[string]any)
		if cond["key"] != "article_id" {
			t.Fatalf("filter sai key: %v", cond)
		}
		match, _ := cond["match"].(map[string]any)
		if match["value"] != float64(9) {
			t.Fatalf("filter sai value: %v", match)
		}
	}
	if !found {
		t.Fatal("không thấy request delete")
	}
}
