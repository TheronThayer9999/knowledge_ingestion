package eino

import (
	"os"

	"knowledge_ingestion/src/common/logs"
	"knowledge_ingestion/src/domain"
)

// Log chẩn đoán chat TẠM THỜI — bật bằng CHAT_DEBUG=1, gỡ sau khi tìm ra
// lý do trả lời sai (retrieval thiếu đoạn, agent đọc lệch, hay nhãn sai).

func chatDebugOn() bool {
	return os.Getenv("CHAT_DEBUG") == "1"
}

func dbgHead(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n])
}

func logSearchDebug(query string, limit int, categories []int64, chunks []*domain.ScoredChunk) {
	if !chatDebugOn() {
		return
	}
	logs.Infow("chatdbg: knowledge_search", "query", query, "limit", limit,
		"categories", categories, "hits", len(chunks))
	for i, h := range chunks {
		if h == nil {
			continue
		}
		logs.Infow("chatdbg: search hit", "idx", i, "article_id", h.ArticleID, "page", h.PageNum,
			"chunk_index", h.ChunkIndex, "score", h.Score, "section", h.Section,
			"text", h.Text)
	}
}

func logReadContextDebug(articleID int64, chunkIndex, window int, chunks []*domain.ScoredChunk) {
	if !chatDebugOn() {
		return
	}
	logs.Infow("chatdbg: read_context", "article_id", articleID, "chunk_index", chunkIndex,
		"window", window, "returned", len(chunks))
	for _, h := range chunks {
		if h == nil {
			continue
		}
		logs.Infow("chatdbg: context chunk", "article_id", h.ArticleID, "chunk_index", h.ChunkIndex,
			"section", h.Section, "text", h.Text)
	}
}

func logToolCallDebug(name, args string) {
	if !chatDebugOn() {
		return
	}
	logs.Infow("chatdbg: tool call", "tool", name, "args", dbgHead(args, 200))
}

func logAnswerDebug(answer string, hits int) {
	if !chatDebugOn() {
		return
	}
	logs.Infow("chatdbg: answer", "hits", hits, "answer_head", dbgHead(answer, 200))
}
