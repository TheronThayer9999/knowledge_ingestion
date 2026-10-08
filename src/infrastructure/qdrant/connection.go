package qdrant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/config"
	"knowledge_ingestion/src/domain"
)

// CollectionName là collection duy nhất worker dùng — 1 collection cho mọi
// article, phân biệt bằng payload article_id (filter + xóa theo bài).
const CollectionName = "article_chunks"

// compile-time check: connection phải implement domain.IVectorStore
var _ domain.IVectorStore = (*connection)(nil)

type connection struct {
	baseURL string // http://host:port (REST 6333, không phải gRPC 6334)
	token   string
	dim     int
	http    *http.Client
}

type upsertPoint struct {
	ID      string         `json:"id"` // uuid string — Qdrant chấp nhận
	Vector  []float32      `json:"vector"`
	Payload map[string]any `json:"payload"`
}

type collectionInfo struct {
	Result struct {
		Config struct {
			Params struct {
				Vectors struct {
					Size     int    `json:"size"`
					Distance string `json:"distance"`
				} `json:"vectors"`
			} `json:"params"`
		} `json:"config"`
	} `json:"result"`
}

func NewConnection(cfg config.IConfig) (domain.IVectorStore, error) {
	q := cfg.GetQdrant()
	if q.Host == "" {
		return nil, fmt.Errorf("qdrant: host is required")
	}
	dim := cfg.GetEmbedding().Dim
	if dim <= 0 {
		return nil, fmt.Errorf("qdrant: embedding dim must be > 0")
	}
	c := newClient(fmt.Sprintf("http://%s:%d", q.Host, q.Port), q.Token, dim)

	// Lifetime-init: collection phải tồn tại đúng dim trước khi worker chạy —
	// fail-fast giống postgres/S3 để không boot trong trạng thái embed mù.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.ensureCollection(ctx); err != nil {
		return nil, err
	}

	logs.Infow("qdrant ready", "collection", CollectionName, "dim", dim)
	return c, nil
}

// newClient dựng client thuần túy (không I/O) — tách riêng để unit test với
// httptest mà không cần Qdrant chạy.
func newClient(baseURL, token string, dim int) *connection {
	return &connection{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		dim:     dim,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// ensureCollection: có rồi thì verify đúng dim (sai dim là config lệch với
// model embed — fail-fast thay vì upsert lỗi hàng loạt), chưa có thì tạo
// Cosine + index payload article_id để xóa theo bài nhanh.
func (c *connection) ensureCollection(ctx context.Context) error {
	var info collectionInfo
	if err := c.doJSON(ctx, http.MethodGet, "/collections/"+CollectionName, nil, &info); err != nil {
		if !isNotFound(err) {
			return err
		}
		body := map[string]any{
			"vectors": map[string]any{"size": c.dim, "distance": "Cosine"},
		}
		if err := c.doJSON(ctx, http.MethodPut, "/collections/"+CollectionName, body, nil); err != nil {
			return fmt.Errorf("qdrant: create collection %s: %w", CollectionName, err)
		}
		logs.Infow("qdrant collection created", "collection", CollectionName)
	} else if info.Result.Config.Params.Vectors.Size != c.dim {
		return fmt.Errorf("qdrant: collection %s có dim %d, config expects %d",
			CollectionName, info.Result.Config.Params.Vectors.Size, c.dim)
	}
	// Index payload best-effort — có rồi (409) hay lỗi cũng không chặn boot,
	// chỉ khiến xóa theo bài chậm hơn.
	indexBody := map[string]any{"field_name": "article_id", "field_schema": "integer"}
	if err := c.doJSON(ctx, http.MethodPut, "/collections/"+CollectionName+"/index", indexBody, nil); err != nil {
		logs.Warnw("qdrant: tạo payload index thất bại (bỏ qua)", "error", err)
	}
	return nil
}

// UpsertPoints ghi đè point theo ID — idempotent nên embed chạy lại vẫn hội tụ.
func (c *connection) UpsertPoints(ctx context.Context, points []domain.VectorPoint) error {
	if len(points) == 0 {
		return nil
	}
	body := map[string]any{"points": make([]upsertPoint, 0, len(points))}
	for _, p := range points {
		if len(p.Vector) != c.dim {
			return fmt.Errorf("%w: point %s có dim %d, collection expects %d", ErrBadRequest, p.ID, len(p.Vector), c.dim)
		}
		body["points"] = append(body["points"].([]upsertPoint), upsertPoint{
			ID:     p.ID,
			Vector: p.Vector,
			Payload: map[string]any{
				"article_id":  p.ArticleID,
				"user_id":     p.UserID,
				"category_id": p.CategoryID,
				"chunk_index": p.ChunkIndex,
				"page_num":    p.PageNum,
				"text":        p.Text,
			},
		})
	}
	if err := c.doJSON(ctx, http.MethodPut, "/collections/"+CollectionName+"/points?wait=true", body, nil); err != nil {
		return fmt.Errorf("qdrant: upsert %d points: %w", len(points), err)
	}
	return nil
}

// DeleteByArticle xóa toàn bộ point của 1 bài — Qdrant filter khớp 0 point
// cũng báo thành công nên 2 worker cùng dọn vẫn an toàn.
func (c *connection) DeleteByArticle(ctx context.Context, articleID int64) error {
	body := map[string]any{
		"filter": map[string]any{
			"must": []any{
				map[string]any{"key": "article_id", "match": map[string]any{"value": articleID}},
			},
		},
	}
	if err := c.doJSON(ctx, http.MethodPost, "/collections/"+CollectionName+"/points/delete?wait=true", body, nil); err != nil {
		return fmt.Errorf("qdrant: delete points của article %d: %w", articleID, err)
	}
	return nil
}

// CountByArticle đếm point của 1 bài — worker embed guard nhánh done-rỗng.
func (c *connection) CountByArticle(ctx context.Context, articleID int64) (int64, error) {
	body := map[string]any{
		"filter": map[string]any{
			"must": []any{
				map[string]any{"key": "article_id", "match": map[string]any{"value": articleID}},
			},
		},
	}
	var out struct {
		Result struct {
			Count int64 `json:"count"`
		} `json:"result"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/collections/"+CollectionName+"/points/count", body, &out); err != nil {
		return 0, fmt.Errorf("qdrant: count points của article %d: %w", articleID, err)
	}
	return out.Result.Count, nil
}

func (c *connection) IsPermanentError(err error) bool { return IsPermanent(err) }

// statusError giữ HTTP status để caller phân biệt 404 (chưa có collection)
// với lỗi thật.
type statusError struct {
	status int
	msg    string
}

func (e *statusError) Error() string { return e.msg }

// ErrBadRequest báo request sai từ phía client (sai dim) — retry vô ích.
var ErrBadRequest = errors.New("qdrant: bad request")

// IsPermanent báo lỗi Qdrant có retry cũng vậy không: 4xx là request sai
// (sai dim, collection/field không khớp) — trừ 408 timeout và 429 nghẽn thì
// vẫn transient. Service gặp lỗi này thì failed luôn thay vì backoff 10 lần.
func IsPermanent(err error) bool {
	if errors.Is(err, ErrBadRequest) {
		return true
	}
	var se *statusError
	if !errors.As(err, &se) {
		return false
	}
	if se.status == http.StatusRequestTimeout || se.status == http.StatusTooManyRequests {
		return false
	}
	return se.status >= 400 && se.status < 500
}

func isNotFound(err error) bool {
	var se *statusError
	return errors.As(err, &se) && se.status == http.StatusNotFound
}

// doJSON gọi REST Qdrant: gắn api-key nếu có, body JSON, status != 200 là lỗi.
// out nil thì bỏ qua decode (dùng cho create/upsert/delete).
func (c *connection) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("qdrant: marshal request: %w", err)
		}
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, r)
	if err != nil {
		return fmt.Errorf("qdrant: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("api-key", c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant: call %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return &statusError{status: resp.StatusCode,
			msg: fmt.Sprintf("qdrant: %s %s returned %d: %s", method, path, resp.StatusCode, msg)}
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("qdrant: decode response: %w", err)
		}
	}
	return nil
}
