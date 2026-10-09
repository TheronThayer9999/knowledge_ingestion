package utils

import (
	"reflect"
	"testing"
)

func TestBatch(t *testing.T) {
	got := Batch([]int{1, 2, 3, 4, 5}, 2)
	want := [][]int{{1, 2}, {3, 4}, {5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := Batch([]int{1, 2}, 10); !reflect.DeepEqual(got, [][]int{{1, 2}}) {
		t.Fatalf("size lớn hơn slice phải trả 1 đợt, got %v", got)
	}
	if got := Batch[int](nil, 2); got != nil {
		t.Fatalf("nil phải trả nil, got %v", got)
	}
	if got := Batch([]int{1, 2}, 0); !reflect.DeepEqual(got, [][]int{{1, 2}}) {
		t.Fatalf("size <= 0 phải trả 1 đợt, got %v", got)
	}
}
