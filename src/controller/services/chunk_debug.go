package services

import (
	"os"

	"knowledge_ingestion/src/common/chunker"
	"knowledge_ingestion/src/common/logs"
)

// Log chẩn đoán chunk TẠM THỜI — bật bằng CHUNK_DEBUG=1, gỡ sau khi tìm
// ra chỗ mất text (extract hay splitter).

func chunkDebugOn() bool {
	return os.Getenv("CHUNK_DEBUG") == "1"
}

func dbgSnip(s string, n int, tail bool) string {
	r := []rune(s)
	if len(r) <= n {
		return string(r)
	}
	if tail {
		return string(r[len(r)-n:])
	}
	return string(r[:n])
}

func logPageDebug(articleID int64, pageNum int, text string) {
	if !chunkDebugOn() {
		return
	}
	logs.Infow("chunkdbg: trang extract", "article_id", articleID, "page", pageNum,
		"runes", len([]rune(text)), "head", dbgSnip(text, 80, false), "tail", dbgSnip(text, 80, true))
}

func logSplitDebug(articleID int64, pageNum int, parts []string) {
	if !chunkDebugOn() {
		return
	}
	for i, p := range parts {
		logs.Infow("chunkdbg: splitter part", "article_id", articleID, "page", pageNum,
			"part", i, "runes", len([]rune(p)), "head", dbgSnip(p, 60, false), "tail", dbgSnip(p, 40, true))
	}
}

func logAnnotateDebug(articleID int64, pageNum int, startIndex int, annotated []chunker.SectionedChunk) {
	if !chunkDebugOn() {
		return
	}
	for i, a := range annotated {
		logs.Infow("chunkdbg: annotated", "article_id", articleID, "page", pageNum,
			"chunk_index", startIndex+i, "section", a.Section, "runes", len([]rune(a.Text)),
			"head", dbgSnip(a.Text, 60, false))
	}
}
