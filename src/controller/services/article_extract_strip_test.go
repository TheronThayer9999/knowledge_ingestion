package services

import "testing"

func TestStripNULRemovesZeroBytes(t *testing.T) {
	in := "Điều 1\x00 nội dung\x00\x00 tiếp"
	want := "Điều 1 nội dung tiếp"
	if got := stripNUL(in); got != want {
		t.Fatalf("stripNUL = %q, muốn %q", got, want)
	}
}

func TestStripNULKeepsCleanText(t *testing.T) {
	in := "Văn bản không có NUL\nDòng hai"
	if got := stripNUL(in); got != in {
		t.Fatalf("stripNUL đổi text sạch: %q", got)
	}
}
